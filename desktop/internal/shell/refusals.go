package shell

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/Hikyo-Org/hikyo/api"
)

// Rotation classifies the routes whose response may carry a new session token
// (ADR D4). Everything else that returns one is refused.
type Rotation int

const (
	// NotRotating is a route the shell never takes a token from.
	NotRotating Rotation = iota
	// Rotate keeps the session id (POST totp/step-up, POST reauth/totp).
	Rotate
	// Reissue deletes every session of the principal and creates a new one.
	Reissue
)

const apiPrefix = api.PathPrefix + "/"

// RotationRoutes is the ADR's rotate and reissue list. Paths are contract
// templates, where a brace segment matches one path segment.
var RotationRoutes = []struct {
	Method, Path string
	Kind         Rotation
}{
	{http.MethodPost, "/api/v1/auth/totp/step-up", Rotate},
	{http.MethodPost, "/api/v1/auth/reauth/totp", Rotate},
	{http.MethodPut, "/api/v1/auth/password", Reissue},
	{http.MethodPost, "/api/v1/auth/totp/enrol/confirm", Reissue},
	{http.MethodDelete, "/api/v1/auth/totp", Reissue},
	{http.MethodPost, "/api/v1/auth/recovery-codes/regenerate", Reissue},
	{http.MethodDelete, "/api/v1/auth/identities/{id}", Reissue},
	{http.MethodDelete, "/api/v1/auth/webauthn/credentials/{id}", Reissue},
}

// refusedPrefixes are refused for every method, whatever the contract says
// (ADR D4): the shell calls these handoffs itself or the app hides them.
var refusedPrefixes = []string{
	"/api/v1/auth/cli-login/",
	"/api/v1/auth/cli-reauth/",
	"/api/v1/auth/workspace/",
}

// developerCredentialMint is refused outright: mint stays a terminal action.
const developerCredentialMint = "/api/v1/orgs/{org}/projects/{project}/environments/{environment}/developer-credentials"

// routeKey is a method and a path template.
type routeKey struct {
	method string
	segs   []string
}

func newRouteKey(method, template string) routeKey {
	return routeKey{method: method, segs: strings.Split(strings.Trim(template, "/"), "/")}
}

// matches compares literal segments case-insensitively: if the instance's
// router were lenient about case, a differently cased spelling must not be a
// way around a refusal.
func (k routeKey) matches(method string, segs []string) bool {
	if k.method != method || len(k.segs) != len(segs) {
		return false
	}
	for i, want := range k.segs {
		if strings.HasPrefix(want, "{") {
			if segs[i] == "" {
				return false
			}
			continue
		}
		if !strings.EqualFold(want, segs[i]) {
			return false
		}
	}
	return true
}

func (k routeKey) String() string { return k.method + " /" + strings.Join(k.segs, "/") }

// Verdict is how the proxy treats a page request to the API.
type Verdict int

const (
	// Allowed passes the request through.
	Allowed Verdict = iota
	// Refused answers 403 without calling the instance.
	Refused
	// Malformed answers 400: a non-canonical /api/v1 path.
	Malformed
)

// Policy classifies requests against the contract. It is immutable.
type Policy struct {
	// sessionRoutes are the contract's operations that return a
	// session_token, including the rotation list.
	sessionRoutes []routeKey
	rotation      []rotationRoute
}

type rotationRoute struct {
	key  routeKey
	kind Rotation
}

var (
	policyOnce sync.Once
	policy     *Policy
	policyErr  error
)

// DefaultPolicy builds the policy from the embedded API contract once, so a
// login route added to the contract is refused without a shell change.
func DefaultPolicy() (*Policy, error) {
	policyOnce.Do(func() {
		doc, err := api.Doc()
		if err != nil {
			policyErr = err
			return
		}
		policy, policyErr = NewPolicy(doc)
	})
	return policy, policyErr
}

// NewPolicy derives the refusal list from a contract document.
func NewPolicy(doc *openapi3.T) (*Policy, error) {
	p := &Policy{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if returnsSessionToken(op) {
				p.sessionRoutes = append(p.sessionRoutes, newRouteKey(method, path))
			}
		}
	}
	if len(p.sessionRoutes) == 0 {
		return nil, errors.New("the API contract lists no session-token routes; refusing to build an empty refusal list")
	}
	for _, r := range RotationRoutes {
		p.rotation = append(p.rotation, rotationRoute{newRouteKey(r.Method, r.Path), r.Kind})
	}
	return p, nil
}

// SessionRoutes lists the contract's session-token operations as
// "METHOD /template", for the drift test.
func (p *Policy) SessionRoutes() []string {
	out := make([]string, 0, len(p.sessionRoutes))
	for _, k := range p.sessionRoutes {
		out = append(out, k.String())
	}
	return out
}

// returnsSessionToken reports whether a 2xx response of op carries
// session_token at the top level or under `login`, the two places the shell
// reads (ADR D4).
func returnsSessionToken(op *openapi3.Operation) bool {
	if op.Responses == nil {
		return false
	}
	for code, ref := range op.Responses.Map() {
		if ref == nil || ref.Value == nil || !strings.HasPrefix(code, "2") {
			continue
		}
		for _, media := range ref.Value.Content {
			if schemaHasToken(media.Schema, map[visit]bool{}, true) {
				return true
			}
		}
	}
	return false
}

// visit is a schema reached in one descent mode. A schema first reached under
// `login` (which is not searched further) must still be searched when a later
// branch reaches it at the response root.
type visit struct {
	schema  *openapi3.Schema
	descend bool
}

func schemaHasToken(ref *openapi3.SchemaRef, seen map[visit]bool, descend bool) bool {
	if ref == nil || ref.Value == nil || seen[visit{ref.Value, descend}] {
		return false
	}
	seen[visit{ref.Value, descend}] = true
	schema := ref.Value
	if _, ok := schema.Properties["session_token"]; ok {
		return true
	}
	for _, group := range [][]*openapi3.SchemaRef{schema.AllOf, schema.OneOf, schema.AnyOf} {
		for _, sub := range group {
			if schemaHasToken(sub, seen, descend) {
				return true
			}
		}
	}
	if login, ok := schema.Properties["login"]; ok && descend {
		return schemaHasToken(login, seen, false)
	}
	return false
}

// Segments canonicalizes a request path for matching. It refuses anything the
// instance's router might read differently from the shell: dot segments, empty
// segments, backslashes, encoded separators, control characters and invalid
// percent-encoding.
func Segments(rawPath string) ([]string, bool) {
	if rawPath == "" || rawPath[0] != '/' {
		return nil, false
	}
	lower := strings.ToLower(rawPath)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return nil, false
	}
	decoded, err := url.PathUnescape(rawPath)
	if err != nil {
		return nil, false
	}
	for _, r := range decoded {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return nil, false
		}
	}
	parts := strings.Split(decoded[1:], "/")
	for _, s := range parts {
		if s == "" || s == "." || s == ".." {
			return nil, false
		}
	}
	return parts, true
}

// IsAPIPath reports whether a path is under /api/v1, case-insensitively, so a
// differently cased spelling is still held to the API rules.
func IsAPIPath(rawPath string) bool {
	lower := strings.ToLower(rawPath)
	return lower == api.PathPrefix || strings.HasPrefix(lower, apiPrefix)
}

// Classify decides whether a page request may reach the instance. rawPath is
// the request's escaped path. Non-API paths are the instance's SPA assets.
func (p *Policy) Classify(method, rawPath string) Verdict {
	if !IsAPIPath(rawPath) {
		// A spelling that is not under /api/v1 as written but decodes or
		// cleans to it (%61pi, /./api, /x/../api, \api) is something the
		// instance's router could route to the API, so it is refused here.
		if reachesAPI(rawPath) {
			return Malformed
		}
		return Allowed
	}
	segs, ok := Segments(rawPath)
	if !ok {
		return Malformed
	}
	lower := strings.ToLower("/"+strings.Join(segs, "/")) + "/"
	for _, prefix := range refusedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return Refused
		}
	}
	if newRouteKey(http.MethodPost, developerCredentialMint).matches(method, segs) {
		return Refused
	}
	if p.rotationOf(method, segs) != NotRotating {
		return Allowed
	}
	for _, r := range p.sessionRoutes {
		if r.matches(method, segs) {
			return Refused
		}
	}
	return Allowed
}

// reachesAPI reports whether a non-API-looking path resolves to the API after
// percent-decoding, backslash folding and dot-segment removal.
func reachesAPI(rawPath string) bool {
	decoded := lenientUnescape(rawPath)
	decoded = strings.ReplaceAll(decoded, "\\", "/")
	return IsAPIPath(path.Clean("/" + decoded))
}

// lenientUnescape decodes every valid %XX and leaves a stray % alone, as a
// router that tolerates bad escapes would, so a path like /search/100% is
// judged on what it says rather than refused for its escape.
func lenientUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(v))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (p *Policy) rotationOf(method string, segs []string) Rotation {
	for _, r := range p.rotation {
		if r.key.matches(method, segs) {
			return r.kind
		}
	}
	return NotRotating
}

// RotationFor returns the rotation class of a response from this route.
func (p *Policy) RotationFor(method, rawPath string) Rotation {
	segs, ok := Segments(rawPath)
	if !ok {
		return NotRotating
	}
	return p.rotationOf(method, segs)
}
