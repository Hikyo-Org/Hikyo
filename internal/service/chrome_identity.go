package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"mime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"golang.org/x/image/webp"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The editable chrome identity of an org or project: hue, a short mark and an
// uploaded image. Every bound below is enforced here, in the domain, because
// internal callers do not pass through request validation.
const (
	// MaxChromeImageBytes bounds one stored avatar. The transport applies the
	// same number as its per-route body cap.
	MaxChromeImageBytes = 256 << 10
	// MaxChromeImageDimension bounds width and height in pixels.
	MaxChromeImageDimension = 1024
	// MaxChromeMarkGraphemes is the longest mark: two user-perceived characters
	// (a letter pair or an emoji, ZWJ sequences count as one).
	MaxChromeMarkGraphemes = 2
	maxChromeMarkBytes     = 128
	maxChromeHue           = 359
)

// ChromeIdentity is the identity as the service reports it. Mark nil means the
// monogram is derived from the name; ImageVersion nil means no uploaded image.
type ChromeIdentity struct {
	Hue          int
	Mark         *string
	ImageVersion *string
}

func chromeIdentityOf(i store.ChromeIdentity) ChromeIdentity {
	return ChromeIdentity{Hue: i.Hue, Mark: i.Mark, ImageVersion: i.ImageVersion}
}

// ChromeImage is a stored avatar ready to serve: the exact validated content
// type and its content-hash version.
type ChromeImage struct {
	Bytes       []byte
	ContentType string
	Version     string
}

// checkChromeIdentity validates hue and mark. A nil mark is "derive from name".
func checkChromeIdentity(hue int, mark *string) error {
	if hue < 0 || hue > maxChromeHue {
		return fmt.Errorf("%w: hue must be an integer from 0 to %d", domain.ErrInvalid, maxChromeHue)
	}
	if mark == nil {
		return nil
	}
	m := *mark
	switch {
	case m == "":
		return fmt.Errorf("%w: mark must not be empty; omit it to use the name monogram", domain.ErrInvalid)
	case len(m) > maxChromeMarkBytes:
		return fmt.Errorf("%w: mark exceeds %d bytes", domain.ErrInvalid, maxChromeMarkBytes)
	case !utf8.ValidString(m):
		return fmt.Errorf("%w: mark is not valid UTF-8", domain.ErrInvalid)
	case strings.TrimSpace(m) != m:
		return fmt.Errorf("%w: mark has leading or trailing whitespace", domain.ErrInvalid)
	}
	for _, r := range m {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: mark contains a control character", domain.ErrInvalid)
		}
	}
	if uniseg.GraphemeClusterCount(m) > MaxChromeMarkGraphemes {
		return fmt.Errorf("%w: mark is at most %d characters", domain.ErrInvalid, MaxChromeMarkGraphemes)
	}
	return nil
}

// chromeImageTypes maps each accepted content type to its magic-byte check and
// header decoder. SVG, and anything else, is absent on purpose: the browser
// rasterises SVG to PNG before upload and the server never stores it.
var chromeImageTypes = map[string]struct {
	magic  func([]byte) bool
	config func(*bytes.Reader) (image.Config, error)
}{
	"image/png": {
		magic:  func(b []byte) bool { return bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) },
		config: func(r *bytes.Reader) (image.Config, error) { return png.DecodeConfig(r) },
	},
	"image/jpeg": {
		magic:  func(b []byte) bool { return bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) },
		config: func(r *bytes.Reader) (image.Config, error) { return jpeg.DecodeConfig(r) },
	},
	"image/webp": {
		magic: func(b []byte) bool {
			return len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
		},
		config: func(r *bytes.Reader) (image.Config, error) { return webp.DecodeConfig(r) },
	},
}

// inspectChromeImage validates an upload without processing it: declared type
// in the closed set, magic bytes matching it, a header that decodes, bounded
// dimensions and size. Only the config is decoded, never the pixels.
func inspectChromeImage(declared string, body []byte) (store.ChromeImage, error) {
	contentType, params, err := mime.ParseMediaType(declared)
	if err != nil || len(params) != 0 {
		return store.ChromeImage{}, fmt.Errorf("%w: image content type must be image/png, image/jpeg or image/webp", domain.ErrInvalid)
	}
	spec, ok := chromeImageTypes[contentType]
	if !ok {
		return store.ChromeImage{}, fmt.Errorf("%w: image content type must be image/png, image/jpeg or image/webp", domain.ErrInvalid)
	}
	if len(body) == 0 || len(body) > MaxChromeImageBytes {
		return store.ChromeImage{}, fmt.Errorf("%w: image must be between 1 byte and %d bytes", domain.ErrInvalid, MaxChromeImageBytes)
	}
	if !spec.magic(body) {
		return store.ChromeImage{}, fmt.Errorf("%w: image bytes do not match the declared content type", domain.ErrInvalid)
	}
	cfg, err := spec.config(bytes.NewReader(body))
	if err != nil {
		return store.ChromeImage{}, fmt.Errorf("%w: image header does not decode", domain.ErrInvalid)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxChromeImageDimension || cfg.Height > MaxChromeImageDimension {
		return store.ChromeImage{}, fmt.Errorf("%w: image must be at most %dx%d pixels", domain.ErrInvalid, MaxChromeImageDimension, MaxChromeImageDimension)
	}
	sum := sha256.Sum256(body)
	return store.ChromeImage{Bytes: body, ContentType: contentType, Version: hex.EncodeToString(sum[:])}, nil
}

// identityChangePayload is the audit record of one identity edit: previous and
// new hue and mark, and previous and new image content-hash version. The bytes
// are never recorded.
func identityChangePayload(before, after store.ChromeIdentity) audit.Payload {
	p := audit.Payload{"previous_hue": before.Hue, "hue": after.Hue}
	for _, f := range []struct {
		key string
		v   *string
		raw bool
	}{
		{"previous_mark", before.Mark, false}, {"mark", after.Mark, false},
		{"previous_image_version", before.ImageVersion, true}, {"image_version", after.ImageVersion, true},
	} {
		if f.v == nil {
			continue
		}
		if f.raw {
			p[f.key] = *f.v
		} else {
			p[f.key] = audit.SanitizeFreeText(*f.v)
		}
	}
	return p
}

// SetIdentity replaces the org's hue and mark. The previous row is read in the
// same transaction as the write so the trail records the real transition.
func (s *Orgs) SetIdentity(ctx context.Context, actor Actor, org domain.OrgID, hue int, mark *string) (Org, error) {
	if err := checkChromeIdentity(hue, mark); err != nil {
		return Org{}, err
	}
	return s.changeIdentity(ctx, actor, org, authz.OpOrgIdentitySet, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Org) error {
		if err := r.Orgs().SetIdentity(ctx, p, hue, mark); err != nil {
			return err
		}
		out.Identity.Hue, out.Identity.Mark = hue, mark
		return nil
	})
}

// SetIdentityImage stores an uploaded avatar after inspecting it.
func (s *Orgs) SetIdentityImage(ctx context.Context, actor Actor, org domain.OrgID, contentType string, body []byte) (Org, error) {
	img, err := inspectChromeImage(contentType, body)
	if err != nil {
		return Org{}, err
	}
	return s.changeIdentity(ctx, actor, org, authz.OpOrgIdentityImageSet, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Org) error {
		if err := r.Orgs().SetIdentityImage(ctx, p, &img); err != nil {
			return err
		}
		out.Identity.ImageVersion = &img.Version
		return nil
	})
}

// ClearIdentityImage removes the uploaded avatar.
func (s *Orgs) ClearIdentityImage(ctx context.Context, actor Actor, org domain.OrgID) (Org, error) {
	return s.changeIdentity(ctx, actor, org, authz.OpOrgIdentityImageClear, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Org) error {
		if err := r.Orgs().SetIdentityImage(ctx, p, nil); err != nil {
			return err
		}
		out.Identity.ImageVersion = nil
		return nil
	})
}

func (s *Orgs) changeIdentity(ctx context.Context, actor Actor, org domain.OrgID, op authz.Operation, apply func(context.Context, store.Repos, authz.Proof, *store.Org) error) (Org, error) {
	var out store.Org
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, op, domain.Scope{Org: org}, time.Now().UTC())
		if err != nil {
			return err
		}
		before, err := r.Orgs().Get(ctx, p)
		if err != nil {
			return err
		}
		out = before
		if err := apply(ctx, r, p, &out); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventOrgIdentityChanged, caller.Principal,
			audit.Object{Type: "org", ID: before.ID}, identityChangePayload(before.Identity, out.Identity))
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	if err != nil {
		return Org{}, err
	}
	return orgOf(out), nil
}

// IdentityImage reads the org's avatar for the rail. It follows listMyOrgs
// visibility, not an authz formula: the rail shows every org that route lists,
// including to a member who holds only a project grant and so lacks read@org.
// resolveSelf admits the same artifacts as ListMine (no machine credential),
// OrgImageVisibleTo applies the same grant/rule projection and self-config
// eligibility, and anything else, including no stored image, is the uniform
// not-found. Nothing is audited, for the reason ListMine is not.
func (s *Orgs) IdentityImage(ctx context.Context, actor Actor, org domain.OrgID) (ChromeImage, error) {
	var img authz.OrgImage
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolveSelf(ctx, az, time.Now().UTC())
		if err != nil {
			return err
		}
		img, err = az.OrgImageVisibleTo(ctx, caller, org)
		return err
	})
	if err != nil {
		return ChromeImage{}, err
	}
	return ChromeImage{Bytes: img.Bytes, ContentType: img.ContentType, Version: img.Version}, nil
}

// SetIdentity replaces the project's hue and mark.
func (s *Projects) SetIdentity(ctx context.Context, actor Actor, scope domain.Scope, hue int, mark *string) (Project, error) {
	if err := checkChromeIdentity(hue, mark); err != nil {
		return Project{}, err
	}
	return s.changeIdentity(ctx, actor, scope, authz.OpProjectIdentitySet, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Project) error {
		if err := r.Projects().SetIdentity(ctx, p, hue, mark); err != nil {
			return err
		}
		out.Identity.Hue, out.Identity.Mark = hue, mark
		return nil
	})
}

// SetIdentityImage stores an uploaded avatar after inspecting it.
func (s *Projects) SetIdentityImage(ctx context.Context, actor Actor, scope domain.Scope, contentType string, body []byte) (Project, error) {
	img, err := inspectChromeImage(contentType, body)
	if err != nil {
		return Project{}, err
	}
	return s.changeIdentity(ctx, actor, scope, authz.OpProjectIdentityImageSet, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Project) error {
		if err := r.Projects().SetIdentityImage(ctx, p, &img); err != nil {
			return err
		}
		out.Identity.ImageVersion = &img.Version
		return nil
	})
}

// ClearIdentityImage removes the uploaded avatar.
func (s *Projects) ClearIdentityImage(ctx context.Context, actor Actor, scope domain.Scope) (Project, error) {
	return s.changeIdentity(ctx, actor, scope, authz.OpProjectIdentityImageClear, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Project) error {
		if err := r.Projects().SetIdentityImage(ctx, p, nil); err != nil {
			return err
		}
		out.Identity.ImageVersion = nil
		return nil
	})
}

func (s *Projects) changeIdentity(ctx context.Context, actor Actor, scope domain.Scope, op authz.Operation, apply func(context.Context, store.Repos, authz.Proof, *store.Project) error) (Project, error) {
	var out store.Project
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, op, scope, time.Now().UTC())
		if err != nil {
			return err
		}
		before, err := r.Projects().Get(ctx, p)
		if err != nil {
			return err
		}
		out = before
		if err := apply(ctx, r, p, &out); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventProjectIdentityChanged, caller.Principal,
			audit.Object{Type: "project", ID: before.ID}, identityChangePayload(before.Identity, out.Identity))
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
	if err != nil {
		return Project{}, err
	}
	return projectOf(out), nil
}

// IdentityImage reads the project's avatar under read@project.
func (s *Projects) IdentityImage(ctx context.Context, actor Actor, scope domain.Scope) (ChromeImage, error) {
	var img store.ChromeImage
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpProjectIdentityImageGet, scope, time.Now().UTC())
		if err != nil {
			return err
		}
		img, err = r.Projects().GetIdentityImage(ctx, p)
		return err
	})
	if err != nil {
		return ChromeImage{}, err
	}
	return ChromeImage{Bytes: img.Bytes, ContentType: img.ContentType, Version: img.Version}, nil
}
