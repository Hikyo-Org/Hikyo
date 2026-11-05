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
	maxChromeJPEGScans     = 100
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

// checkChromeIdentity validates hue and mark and returns the mark to store: nil
// or empty means "derive from name".
func checkChromeIdentity(hue int, mark *string) (*string, error) {
	if hue < 0 || hue > maxChromeHue {
		return nil, fmt.Errorf("%w: hue must be an integer from 0 to %d", domain.ErrInvalid, maxChromeHue)
	}
	if mark == nil || *mark == "" {
		return nil, nil
	}
	m := *mark
	switch {
	case len(m) > maxChromeMarkBytes:
		return nil, fmt.Errorf("%w: mark exceeds %d bytes", domain.ErrInvalid, maxChromeMarkBytes)
	case !utf8.ValidString(m):
		return nil, fmt.Errorf("%w: mark is not valid UTF-8", domain.ErrInvalid)
	case strings.TrimSpace(m) != m:
		return nil, fmt.Errorf("%w: mark has leading or trailing whitespace", domain.ErrInvalid)
	}
	visible := false
	for _, r := range m {
		if unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && !isEmojiJoiner(r)) {
			return nil, fmt.Errorf("%w: mark contains a control or invisible format character", domain.ErrInvalid)
		}
		visible = visible || !(unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r))
	}
	if !visible {
		return nil, fmt.Errorf("%w: mark has nothing visible", domain.ErrInvalid)
	}
	if uniseg.GraphemeClusterCount(m) > MaxChromeMarkGraphemes {
		return nil, fmt.Errorf("%w: mark is at most %d characters", domain.ErrInvalid, MaxChromeMarkGraphemes)
	}
	return mark, nil
}

// isEmojiJoiner admits the format characters real emoji are built from: the ZWJ
// and the tag characters of subdivision flags. U+200B and bidi overrides stay out.
func isEmojiJoiner(r rune) bool {
	return r == 0x200D || (r >= 0xE0020 && r <= 0xE007F)
}

// chromeImageTypes maps each accepted content type to its magic-byte check and
// decoders. SVG, and anything else, is absent on purpose: the browser rasterises
// SVG to PNG before upload and the server never stores it.
var chromeImageTypes = map[string]struct {
	magic  func([]byte) bool
	config func(*bytes.Reader) (image.Config, error)
	decode func(*bytes.Reader) (image.Image, error)
}{
	"image/png": {
		magic:  func(b []byte) bool { return bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) },
		config: func(r *bytes.Reader) (image.Config, error) { return png.DecodeConfig(r) },
		decode: func(r *bytes.Reader) (image.Image, error) { return png.Decode(r) },
	},
	"image/jpeg": {
		magic:  func(b []byte) bool { return bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) },
		config: func(r *bytes.Reader) (image.Config, error) { return jpeg.DecodeConfig(r) },
		decode: func(r *bytes.Reader) (image.Image, error) { return jpeg.Decode(r) },
	},
	"image/webp": {
		magic: func(b []byte) bool {
			return len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
		},
		config: func(r *bytes.Reader) (image.Config, error) { return webp.DecodeConfig(r) },
		decode: func(r *bytes.Reader) (image.Image, error) { return webp.Decode(r) },
	},
}

// inspectChromeImage validates an upload without changing it: declared type in
// the closed set, magic bytes matching it, bounded size and dimensions, and
// pixels that decode. A CSS background gives no load-error signal and the mark
// is hidden while an image is set, so undecodable bytes would be a blank badge.
// The dimensions are checked first, so the decode's memory stays bounded.
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
	// Decode cost grows with progressive JPEG scans, not bytes or pixels. Encoders
	// write about ten; a marker inside metadata only makes this count high.
	if contentType == "image/jpeg" && bytes.Count(body, []byte{0xFF, 0xDA}) > maxChromeJPEGScans {
		return store.ChromeImage{}, fmt.Errorf("%w: JPEG has too many scans", domain.ErrInvalid)
	}
	if _, err := spec.decode(bytes.NewReader(body)); err != nil {
		return store.ChromeImage{}, fmt.Errorf("%w: image does not decode", domain.ErrInvalid)
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
	mark, err := checkChromeIdentity(hue, mark)
	if err != nil {
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

// SetIdentityImage stores an uploaded avatar. It inspects the image only once
// the caller is authorized, because decoding is the expensive step.
func (s *Orgs) SetIdentityImage(ctx context.Context, actor Actor, org domain.OrgID, contentType string, body []byte) (Org, error) {
	return s.changeIdentity(ctx, actor, org, authz.OpOrgIdentityImageSet, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Org) error {
		img, err := inspectChromeImage(contentType, body)
		if err != nil {
			return err
		}
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
// visibility: the rail shows every org that route lists, including to a member
// who holds only a project grant and so lacks read@org. Anyone else who may read
// the org, such as an instance operator on its settings page, is also admitted.
// resolveSelf admits the same artifacts as ListMine (no machine credential), and
// anything else, including no stored image, is the uniform not-found. A successful
// read is not audited, for the reason ListMine is not; a caller outside the
// projection who lacks org.get leaves the usual denial record.
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
	mark, err := checkChromeIdentity(hue, mark)
	if err != nil {
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

// SetIdentityImage stores an uploaded avatar. It inspects the image only once
// the caller is authorized, because decoding is the expensive step.
func (s *Projects) SetIdentityImage(ctx context.Context, actor Actor, scope domain.Scope, contentType string, body []byte) (Project, error) {
	return s.changeIdentity(ctx, actor, scope, authz.OpProjectIdentityImageSet, func(ctx context.Context, r store.Repos, p authz.Proof, out *store.Project) error {
		img, err := inspectChromeImage(contentType, body)
		if err != nil {
			return err
		}
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
