package isolation

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func chromePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func chromeJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestChromeIdentityOrg(t *testing.T) { forEngines(t, runChromeIdentityOrg) }

// runChromeIdentityOrg drives the org identity through the real services on
// both engines: who may write, who may read the image (the listMyOrgs
// audience plus org.get holders), what is refused, what the trail records, and that
// deleting the org takes the image with it.
func runChromeIdentityOrg(t *testing.T, db *store.DB) {
	orgs := &service.Orgs{DB: db}
	ctx := t.Context()
	asAdmin := service.LocalPrincipal(orgAdmin)
	mark := "AB"

	if _, err := orgs.SetIdentity(ctx, service.LocalPrincipal(reader), orgA, 280, &mark); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read-only member set identity: %v, want the uniform nonexistent response", err)
	}
	if _, err := orgs.SetIdentity(ctx, service.LocalPrincipal(nobody), orgA, 280, &mark); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("stranger set identity: %v, want the uniform nonexistent response", err)
	}
	updated, err := orgs.SetIdentity(ctx, asAdmin, orgA, 280, &mark)
	if err != nil {
		t.Fatalf("manage-members set identity: %v", err)
	}
	if updated.Identity.Hue != 280 || updated.Identity.Mark == nil || *updated.Identity.Mark != "AB" {
		t.Fatalf("identity = %+v", updated.Identity)
	}

	for name, bad := range map[string]struct {
		hue  int
		mark string
	}{
		"hue above range": {360, "AB"}, "negative hue": {-1, "AB"},
		"three graphemes": {10, "ABC"}, "control character": {10, "A\x00"}, "surrounding space": {10, " A"},
		"zero-width space": {10, "\u200b"}, "bidi override": {10, "A\u202e"}, "lone variation selector": {10, "\ufe0f"},
		"lone joiner": {10, "\u200d"},
	} {
		if _, err := orgs.SetIdentity(ctx, asAdmin, orgA, bad.hue, &bad.mark); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if _, err := orgs.SetIdentity(ctx, asAdmin, orgA, 10, new("🚀")); err != nil {
		t.Errorf("a single emoji is one grapheme: %v", err)
	}
	for name, emoji := range map[string]string{
		"joined": "\U0001F468\u200D\U0001F469\u200D\U0001F467", "subdivision flag": "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F", "keycap": "1\ufe0f\u20e3",
	} {
		if _, err := orgs.SetIdentity(ctx, asAdmin, orgA, 10, &emoji); err != nil {
			t.Errorf("%s emoji: %v", name, err)
		}
	}
	empty := ""
	if cleared, err := orgs.SetIdentity(ctx, asAdmin, orgA, 10, &empty); err != nil || cleared.Identity.Mark != nil {
		t.Errorf("an empty mark selects the monogram: %+v, %v", cleared.Identity, err)
	}

	// Uploads: the closed type set, magic bytes, size and dimensions.
	good := chromePNG(t, 16, 16)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>`)
	for name, upload := range map[string]struct {
		contentType string
		body        []byte
	}{
		"svg declared as svg":  {"image/svg+xml", svg},
		"svg declared as png":  {"image/png", svg},
		"png declared as jpeg": {"image/jpeg", good},
		"gif":                  {"image/gif", []byte("GIF89a")},
		"too many pixels":      {"image/png", chromePNG(t, service.MaxChromeImageDimension+1, 1)},
		"too many bytes":       {"image/png", append(append([]byte{}, good...), make([]byte, service.MaxChromeImageBytes)...)},
		"empty":                {"image/png", nil},
		"pixels do not decode": {"image/png", good[:len(good)-20]},
		"too many JPEG scans":  {"image/jpeg", append(chromeJPEG(t), bytes.Repeat([]byte{0xFF, 0xDA}, 101)...)},
	} {
		if _, err := orgs.SetIdentityImage(ctx, asAdmin, orgA, upload.contentType, upload.body); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if _, err := orgs.SetIdentityImage(ctx, service.LocalPrincipal(reader), orgA, "image/png", good); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read-only member uploaded: %v", err)
	}
	// Authorization comes first, so an unauthorized caller never reaches the
	// expensive decode: bad bytes are the uniform refusal, not a validation error.
	if _, err := orgs.SetIdentityImage(ctx, service.LocalPrincipal(reader), orgA, "image/png", []byte("not an image")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read-only member with bad bytes: %v, want the uniform refusal", err)
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	for contentType, body := range map[string][]byte{"image/jpeg": chromeJPEG(t), "image/webp": webp} {
		if _, err := orgs.SetIdentityImage(ctx, asAdmin, orgA, contentType, body); err != nil {
			t.Errorf("%s upload: %v", contentType, err)
		}
	}

	withImage, err := orgs.SetIdentityImage(ctx, asAdmin, orgA, "image/png", good)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	version := withImage.Identity.ImageVersion
	if version == nil || len(*version) != 64 {
		t.Fatalf("image version = %v, want a sha256 hex digest", version)
	}

	// The image audience is the listMyOrgs audience: a member holding only a
	// project-scoped grant sees it (they lack read@org), a stranger does not,
	// and a stranger's refusal is the same bytes as a missing image.
	for name, reading := range map[string]domain.PrincipalID{"org admin": orgAdmin, "reader": reader, "project-scoped member": prjAdmin} {
		img, err := orgs.IdentityImage(ctx, service.LocalPrincipal(reading), orgA)
		if err != nil {
			t.Fatalf("%s reading the org image: %v", name, err)
		}
		if img.ContentType != "image/png" || !bytes.Equal(img.Bytes, good) || img.Version != *version {
			t.Fatalf("%s: image = %q %d bytes version %q", name, img.ContentType, len(img.Bytes), img.Version)
		}
	}
	_, hidden := orgs.IdentityImage(ctx, service.LocalPrincipal(bob), orgA)
	_, absent := orgs.IdentityImage(ctx, service.LocalPrincipal(bob), orgB)
	assertUniformNotFound(t, hidden, absent)
	if _, err := orgs.IdentityImage(ctx, service.LocalPrincipal(nobody), orgA); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("stranger read the org image: %v", err)
	}

	// The list and get surfaces carry the version and never the bytes.
	mine, err := orgs.ListMine(ctx, service.LocalPrincipal(reader))
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].Identity.ImageVersion == nil || *mine[0].Identity.ImageVersion != *version {
		t.Fatalf("my orgs = %+v", mine)
	}

	cleared, err := orgs.ClearIdentityImage(ctx, asAdmin, orgA)
	if err != nil || cleared.Identity.ImageVersion != nil {
		t.Fatalf("clear = %+v, %v", cleared.Identity, err)
	}
	if _, err := orgs.IdentityImage(ctx, asAdmin, orgA); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("image after clear: %v", err)
	}

	// The trail records transitions and the content-hash version, never bytes.
	trail := queryStrings(t, db, `SELECT payload FROM audit_tenant_events WHERE type = 'settings.org_identity_changed' ORDER BY seq`)
	if !strings.Contains(trail, *version) {
		t.Fatalf("trail does not record the image version: %s", trail)
	}
	for _, leak := range []string{string(good[:8]), "PNG", "iVBOR"} {
		if strings.Contains(trail, leak) {
			t.Fatalf("trail carries image content (%q): %s", leak, trail)
		}
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'settings.org_identity_changed'`); n < 4 {
		t.Fatalf("identity audit rows = %d, want one per accepted write", n)
	}

	// Deleting the org takes its image with it.
	execRaw(t, db, `INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('org_gone', 'org-gone', TRUE, '{}', `+ts+`)`)
	if _, err := orgs.SetIdentityImage(ctx, service.LocalPrincipal(root), "org_gone", "image/png", good); err != nil {
		t.Fatalf("instance operator upload: %v", err)
	}
	// An operator holds no org-scoped grant, so the rail's projection omits them,
	// yet they just uploaded this from the org's settings page and must see it.
	if img, err := orgs.IdentityImage(ctx, service.LocalPrincipal(root), "org_gone"); err != nil || !bytes.Equal(img.Bytes, good) {
		t.Fatalf("instance operator image read: %v", err)
	}
	if err := orgs.Delete(ctx, service.LocalPrincipal(root), "org_gone"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM orgs WHERE id = 'org_gone'`); n != 0 {
		t.Fatalf("deleted org row remains: %d", n)
	}
	if _, err := orgs.IdentityImage(ctx, service.LocalPrincipal(root), "org_gone"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("image of a deleted org: %v", err)
	}
}

func TestChromeIdentityProject(t *testing.T) { forEngines(t, runChromeIdentityProject) }

func runChromeIdentityProject(t *testing.T, db *store.DB) {
	projects := &service.Projects{DB: db}
	ctx := t.Context()
	scope := scopeProject(orgA, prjA1)
	mark := "A1"
	good := chromePNG(t, 8, 8)

	// manage-projects@org writes; read-only and cross-org callers get the uniform refusal.
	if _, err := projects.SetIdentity(ctx, service.LocalPrincipal(reader), scope, 40, &mark); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read-only member set identity: %v", err)
	}
	if _, err := projects.SetIdentity(ctx, service.LocalPrincipal(bob), scope, 40, &mark); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-org set identity: %v", err)
	}
	updated, err := projects.SetIdentity(ctx, service.LocalPrincipal(alice), scope, 40, &mark)
	if err != nil || updated.Identity.Hue != 40 {
		t.Fatalf("manage-projects set identity = %+v, %v", updated.Identity, err)
	}
	if _, err := projects.SetIdentity(ctx, service.LocalPrincipal(alice), scope, 400, &mark); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bad hue: %v", err)
	}
	if _, err := projects.SetIdentityImage(ctx, service.LocalPrincipal(reader), scope, "image/png", []byte("not an image")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read-only member with bad bytes: %v, want the uniform refusal", err)
	}
	if _, err := projects.SetIdentityImage(ctx, service.LocalPrincipal(alice), scope, "image/svg+xml", []byte("<svg/>")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("svg upload: %v", err)
	}
	withImage, err := projects.SetIdentityImage(ctx, service.LocalPrincipal(alice), scope, "image/png", good)
	if err != nil || withImage.Identity.ImageVersion == nil {
		t.Fatalf("upload = %+v, %v", withImage.Identity, err)
	}
	// Whoever can navigate to the project draws its tile: a member reaching it only
	// through one environment holds no read@project, and a refused image is a blank
	// tile because the mark is hidden while an image is set.
	execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_env_only', 'human', `+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_env_only', 'usr_env_only', 'read', 'org_a', 'prj_a1', 'env_a1', `+ts+`)`)
	execRaw(t, db, `INSERT INTO grant_origins (id, grant_id, kind, subject, created_at) VALUES ('gor_env_only', 'g_env_only', 'manual', 'usr_env_only', `+ts+`)`)
	if img, err := projects.IdentityImage(ctx, service.LocalPrincipal("usr_env_only"), scope); err != nil || !bytes.Equal(img.Bytes, good) {
		t.Fatalf("environment-scoped member reading the project image: %v", err)
	}

	// read@project reads the image; a cross-org caller and a missing image are the same refusal.
	img, err := projects.IdentityImage(ctx, service.LocalPrincipal(reader), scope)
	if err != nil || img.ContentType != "image/png" || !bytes.Equal(img.Bytes, good) {
		t.Fatalf("read = %q %v", img.ContentType, err)
	}
	_, hidden := projects.IdentityImage(ctx, service.LocalPrincipal(bob), scope)
	_, absent := projects.IdentityImage(ctx, service.LocalPrincipal(alice), scopeProject(orgA, prjA2))
	assertUniformNotFound(t, hidden, absent)

	if _, err := projects.ClearIdentityImage(ctx, service.LocalPrincipal(alice), scope); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.IdentityImage(ctx, service.LocalPrincipal(alice), scope); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("image after clear: %v", err)
	}
	trail := queryStrings(t, db, `SELECT payload FROM audit_tenant_events WHERE type = 'settings.project_identity_changed' ORDER BY seq`)
	if !strings.Contains(trail, *withImage.Identity.ImageVersion) || strings.Contains(trail, "iVBOR") {
		t.Fatalf("trail = %s", trail)
	}
}

func TestChromeIdentityWire(t *testing.T) { forEngines(t, runChromeIdentityWire) }

// runChromeIdentityWire is the HTTP leg on the real stack: an SVG is refused
// by the server (the browser converts it to PNG first), the stored image is
// served with its exact validated content type, nosniff and the global
// no-store headers untouched, and the raw-body cap is a 400.
func runChromeIdentityWire(t *testing.T, db *store.DB) {
	e := newAccessWireEnv(t, db)
	good := chromePNG(t, 16, 16)
	raw := func(method, path, token, contentType string, body []byte) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, e.srv.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := e.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, out
	}
	orgImage := api.PathPrefix + "/orgs/" + e.org + "/identity/image"
	projectImage := api.PathPrefix + "/orgs/" + e.org + "/projects/" + e.project + "/identity/image"

	for _, path := range []string{orgImage, projectImage} {
		if resp, body := raw(http.MethodPut, path, e.token, "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("SVG upload to %s = %d %s, want 400", path, resp.StatusCode, body)
		}
		if resp, body := raw(http.MethodPut, path, e.token, "image/png", append(append([]byte{}, good...), make([]byte, service.MaxChromeImageBytes)...)); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("over-cap upload to %s = %d %s, want 400", path, resp.StatusCode, body)
		}
		if resp, body := raw(http.MethodPut, path, e.token, "image/png", good); resp.StatusCode != http.StatusOK {
			t.Fatalf("PNG upload to %s = %d %s, want 200", path, resp.StatusCode, body)
		}
		resp, body := raw(http.MethodGet, path, e.token, "", nil)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, good) {
			t.Fatalf("GET %s = %d, %d bytes", path, resp.StatusCode, len(body))
		}
		if got := resp.Header.Get("Content-Type"); got != "image/png" {
			t.Errorf("GET %s Content-Type = %q, want exactly image/png", path, got)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s X-Content-Type-Options = %q, want nosniff", path, got)
		}
		if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
			t.Errorf("GET %s Cache-Control = %q, the global no-store must stay", path, got)
		}
	}
	if resp, _ := raw(http.MethodGet, orgImage, "", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous org image read = %d, want 401", resp.StatusCode)
	}
	// Same setup as the artifact admission test: a real machine credential is
	// refused this human-session route, and just as a missing org is.
	projectScope := domain.Scope{Org: domain.OrgID(e.org), Project: domain.ProjectID(e.project)}
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES (`+
		`'g_chrome_admin', '`+string(e.admin)+`', 'manage-identities', '`+e.org+`', '`+e.project+`', NULL, '2026-01-01T00:00:00Z')`)
	execRaw(t, db, `INSERT INTO grant_origins (id, grant_id, kind, subject, created_at) VALUES (`+
		`'gor_chrome_admin', 'g_chrome_admin', 'manual', '`+string(e.admin)+`', '2026-01-01T00:00:00Z')`)
	identities := &service.Identities{DB: db, Auth: e.auth}
	sa, err := identities.CreateServiceAccount(t.Context(), service.Bearer(e.token), projectScope, "chrome-probe", domain.ClassWorkload)
	if err != nil {
		t.Fatalf("create machine identity: %v", err)
	}
	minted, err := identities.MintCredential(t.Context(), service.Bearer(e.token), projectScope, sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatalf("mint machine credential: %v", err)
	}
	resp, body := raw(http.MethodGet, orgImage, minted.Value, "", nil)
	_, missing := raw(http.MethodGet, api.PathPrefix+"/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fab/identity/image", minted.Value, "", nil)
	if resp.StatusCode != http.StatusNotFound || !bytes.Equal(body, missing) {
		t.Fatalf("machine org image read = %d %s, want the uniform 404 %s", resp.StatusCode, body, missing)
	}
}
