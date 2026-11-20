package shell

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clienterr"
)

// Shell route error codes (docs/spec/desktop-shell-protocol.md).
const (
	codeRefused     = "refused"
	codeBusy        = "busy"
	codeRateLimited = "rate_limited"
	codeCancelled   = "cancelled"
	codeFailed      = "failed"
	codeInvalid     = "invalid"
)

func init() {
	shellStatus[codeCapabilityMissing] = http.StatusUnprocessableEntity
}

var shellStatus = map[string]int{
	codeInvalid:     http.StatusBadRequest,
	codeRefused:     http.StatusForbidden,
	codeBusy:        http.StatusConflict,
	codeCancelled:   http.StatusConflict,
	codeRateLimited: http.StatusTooManyRequests,
	codeFailed:      http.StatusBadGateway,
}

// shellErr is a refusal or failure a shell route reports to the page.
type shellErr struct{ code, message string }

func (e *shellErr) Error() string { return e.code + ": " + e.message }

func marshalError(code, message string) ([]byte, error) {
	return json.Marshal(struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{code, message})
}

func writeShellError(w http.ResponseWriter, e *shellErr) {
	status := shellStatus[e.code]
	if status == 0 {
		status = http.StatusBadGateway
	}
	jsonError(w, status, e.code, e.message)
}

const (
	maxShellBody     = 1 << 20
	maxDownloadBytes = 64 << 20 // decoded bytes_base64 a page may hand over (the SPA's audit export cap)
	maxDownloadFile  = 2 << 30  // a proxied download's size
	maxEnvironments  = 1000
	maxKeyIDs        = 500
)

var idPattern = regexp.MustCompile(`^[a-z]{2,8}_[0-9a-fA-F-]{36}$`)

// serveShell answers the closed list of /__shell routes (ADR D9). Nothing here
// is forwarded to the instance, and every route acts on this connection only.
func (c *Conn) serveShell(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	route := map[string]struct {
		method string
		run    func(http.ResponseWriter, *http.Request) *shellErr
	}{
		"/__shell/reauth":          {http.MethodPost, c.routeReauth},
		"/__shell/sign-in":         {http.MethodPost, c.routeSignIn},
		"/__shell/open-in-browser": {http.MethodPost, c.routeOpenInBrowser},
		"/__shell/download":        {http.MethodPost, c.routeDownload},
		"/__shell/clipboard":       {http.MethodPost, c.routeClipboard},
		"/__shell/origin":          {http.MethodGet, c.routeOrigin},
	}[r.URL.Path]
	if route.run == nil {
		writeShellError(w, &shellErr{codeInvalid, "unknown shell route"})
		return
	}
	if r.Method != route.method {
		w.Header().Set("Allow", route.method)
		jsonError(w, http.StatusMethodNotAllowed, codeInvalid, "wrong method")
		return
	}
	if err := route.run(w, r); err != nil {
		writeShellError(w, err)
		return
	}
}

// decodeBody reads a strict JSON request of at most limit bytes.
func decodeBody(r *http.Request, limit int64, v any) *shellErr {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return &shellErr{codeInvalid, "requests are application/json"}
	}
	data, err := readLimited(r.Body, limit)
	if errors.Is(err, errTooLarge) {
		return &shellErr{codeInvalid, "the request is too large"}
	}
	if err != nil {
		return &shellErr{codeInvalid, "the request could not be read"}
	}
	if trimmed := strings.TrimLeft(string(data), " \t\r\n"); !strings.HasPrefix(trimmed, "{") {
		return &shellErr{codeInvalid, "the request must be a JSON object"}
	}
	if err := decodeStrict(data, v); err != nil {
		return &shellErr{codeInvalid, "the request is not valid for this route"}
	}
	return nil
}

func noContent(w http.ResponseWriter) *shellErr {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (c *Conn) routeOrigin(w http.ResponseWriter, _ *http.Request) *shellErr {
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, struct {
		Origin string `json:"origin"`
	}{c.inst.Origin})
	return nil
}

type emptyBody struct{}

func (c *Conn) routeClipboard(w http.ResponseWriter, r *http.Request) *shellErr {
	var in emptyBody
	if e := decodeBody(r, 1<<10, &in); e != nil {
		return e
	}
	if !c.clipLimit.allow() {
		return &shellErr{codeRateLimited, "too many copies; try again shortly"}
	}
	c.m.clip.arm(c.host)
	return noContent(w)
}

func (c *Conn) routeOpenInBrowser(w http.ResponseWriter, r *http.Request) *shellErr {
	var in struct {
		Path string `json:"path"`
	}
	if e := decodeBody(r, 1<<14, &in); e != nil {
		return e
	}
	target, err := c.BrowserURL(in.Path)
	if err != nil {
		return &shellErr{codeRefused, "that page can't be opened from the app"}
	}
	if !c.openLimit.allow() {
		return &shellErr{codeRateLimited, "too many pages opened; try again shortly"}
	}
	if err := c.m.opts.OpenURL(target); err != nil {
		return &shellErr{codeFailed, "the browser could not be opened"}
	}
	return noContent(w)
}

type reauthIn struct {
	Purpose        string                         `json:"purpose"`
	Operation      string                         `json:"operation"`
	EnvironmentIDs []string                       `json:"environment_ids"`
	KeyIDs         []string                       `json:"key_ids,omitzero"`
	SelfConfig     *apigen.SelfConfigReauthIntent `json:"self_config,omitzero"`
}

// reauthPurposes are the D7 purposes the browser hop serves.
var reauthPurposes = []apigen.CLIReauthStartRequestPurpose{
	apigen.CLIReauthStartRequestPurposeAdapter, apigen.CLIReauthStartRequestPurposeReveal,
	apigen.CLIReauthStartRequestPurposeCopy, apigen.CLIReauthStartRequestPurposePublish,
	apigen.CLIReauthStartRequestPurposeApprove, apigen.CLIReauthStartRequestPurposeReject,
	apigen.CLIReauthStartRequestPurposeBypass, apigen.CLIReauthStartRequestPurposeSelfConfig,
}

func validIDs(ids []string, max int) bool {
	if len(ids) > max {
		return false
	}
	return !slices.ContainsFunc(ids, func(id string) bool { return !idPattern.MatchString(id) })
}

func (in reauthIn) validate() *shellErr {
	purpose := apigen.CLIReauthStartRequestPurpose(in.Purpose)
	switch {
	case purpose == apigen.CLIReauthStartRequestPurposeDeveloperCredential ||
		apigen.CLIReauthStartRequestOperation(in.Operation) == apigen.CLIReauthStartRequestOperationDeveloperCredentialMint:
		return &shellErr{codeRefused, "developer credentials are minted from a terminal"}
	case !slices.Contains(reauthPurposes, purpose) || !apigen.CLIReauthStartRequestOperation(in.Operation).Valid():
		return &shellErr{codeInvalid, "unknown purpose or operation"}
	case !validIDs(in.EnvironmentIDs, maxEnvironments) || !validIDs(in.KeyIDs, maxKeyIDs):
		return &shellErr{codeInvalid, "the environment or key list is invalid"}
	case (purpose == apigen.CLIReauthStartRequestPurposeSelfConfig) != (in.SelfConfig != nil):
		return &shellErr{codeInvalid, "self_config belongs to the self-config purpose only"}
	case purpose == apigen.CLIReauthStartRequestPurposeSelfConfig:
		// The handoff refuses a self-config request that names an environment.
		if len(in.EnvironmentIDs) != 0 || len(in.KeyIDs) != 0 {
			return &shellErr{codeInvalid, "a self-config ceremony takes no environments or keys"}
		}
	case purpose == apigen.CLIReauthStartRequestPurposeAdapter:
		if len(in.EnvironmentIDs) == 0 || len(in.KeyIDs) > 0 {
			return &shellErr{codeInvalid, "an adapter ceremony takes environments and no keys"}
		}
	default: // the key-bound purposes: one environment, the decision's keys
		if len(in.EnvironmentIDs) != 1 || len(in.KeyIDs) == 0 {
			return &shellErr{codeInvalid, "this ceremony takes exactly one environment and its keys"}
		}
	}
	return nil
}

func (c *Conn) routeReauth(w http.ResponseWriter, r *http.Request) *shellErr {
	var in reauthIn
	if e := decodeBody(r, maxShellBody, &in); e != nil {
		return e
	}
	if e := in.validate(); e != nil {
		return e
	}
	if c.Stopped() != "" {
		return &shellErr{codeFailed, "this connection needs a new sign-in first"}
	}
	if !c.reauthLimit.allow() {
		return &shellErr{codeRateLimited, "too many ceremonies; try again shortly"}
	}
	release, ok := c.reauthSlot.take()
	if !ok {
		return &shellErr{codeBusy, "a ceremony is already running on this connection"}
	}
	// The ceremony's lifetime doesn't depend on the page's request (ADR D9):
	// aborting the fetch neither cancels the browser hop nor frees the slot.
	done := make(chan *shellErr, 1)
	c.m.background(func() {
		defer release()
		ctx, cancel := context.WithTimeout(c.life(), 6*time.Minute)
		defer cancel()
		done <- c.runReauth(ctx, in)
	})
	select {
	case e := <-done:
		if e != nil {
			return e
		}
		return noContent(w)
	case <-r.Context().Done():
		return &shellErr{codeCancelled, "the request was abandoned"}
	}
}

func (c *Conn) routeSignIn(w http.ResponseWriter, r *http.Request) *shellErr {
	var in emptyBody
	if e := decodeBody(r, 1<<10, &in); e != nil {
		return e
	}
	if !c.signInLimit.allow() {
		return &shellErr{codeRateLimited, "too many sign-in attempts; try again shortly"}
	}
	release, ok := c.signInSlot.take()
	if !ok {
		return &shellErr{codeBusy, "a sign-in is already running on this connection"}
	}
	done := make(chan *shellErr, 1)
	c.m.background(func() {
		defer release()
		ctx, cancel := context.WithTimeout(c.life(), 11*time.Minute)
		defer cancel()
		done <- c.runSignIn(ctx)
	})
	select {
	case e := <-done:
		if e != nil {
			return e
		}
		return noContent(w)
	case <-r.Context().Done():
		return &shellErr{codeCancelled, "the request was abandoned"}
	}
}

func (c *Conn) routeDownload(w http.ResponseWriter, r *http.Request) *shellErr {
	var in struct {
		Path        string `json:"path,omitzero"`
		BytesBase64 string `json:"bytes_base64,omitzero"`
		ContentType string `json:"content_type,omitzero"`
		Filename    string `json:"filename"`
	}
	// The limiter and the slot come before the body is read, so a page can't
	// make the shell buffer many large bodies at once.
	if !c.downloadLimit.allow() {
		return &shellErr{codeRateLimited, "too many downloads; try again shortly"}
	}
	release, ok := c.downloadSlot.take()
	if !ok {
		return &shellErr{codeBusy, "a download is already waiting on this connection"}
	}
	defer release()
	if e := decodeBody(r, maxDownloadBytes*4/3+4096, &in); e != nil {
		return e
	}
	if (in.Path == "") == (in.BytesBase64 == "") {
		return &shellErr{codeInvalid, "send exactly one of path and bytes_base64"}
	}
	var data []byte
	if in.BytesBase64 != "" {
		var err error
		if data, err = base64.StdEncoding.DecodeString(in.BytesBase64); err != nil || len(data) > maxDownloadBytes {
			return &shellErr{codeInvalid, "bytes_base64 is not valid base64 or is too large"}
		}
	} else if e := c.checkDownloadPath(in.Path); e != nil {
		return e
	}
	mimeType := ""
	if in.ContentType != "" {
		mt, _, err := mime.ParseMediaType(in.ContentType)
		if err != nil {
			return &shellErr{codeInvalid, "content_type is not a media type"}
		}
		mimeType = mt
	}
	return c.saveDownload(r.Context(), w, in.Path, data, in.Filename, "download", mimeType)
}

// checkDownloadPath holds a proxied download to D4's refusals.
func (c *Conn) checkDownloadPath(p string) *shellErr {
	if len(p) > maxBrowserPath || !hasAPIPrefix(p) {
		return &shellErr{codeInvalid, "path must start with /api/v1/"}
	}
	pathOnly := p
	if i := indexAny(p, "?#"); i >= 0 {
		pathOnly = p[:i]
	}
	if c.m.policy.Classify(http.MethodGet, pathOnly) != Allowed {
		return &shellErr{codeRefused, "that download isn't available in the desktop app"}
	}
	return nil
}

func hasAPIPrefix(p string) bool { return len(p) > len(apiPrefix) && p[:len(apiPrefix)] == apiPrefix }

func indexAny(s, chars string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return i
			}
		}
	}
	return -1
}

// failureOf maps an error from a login or ceremony to a shell error without
// echoing instance-controlled text.
func failureOf(err error) *shellErr {
	var limited *clienterr.RateLimitedError
	var coded *clienterr.Error
	switch {
	case errors.Is(err, context.Canceled):
		return &shellErr{codeCancelled, "cancelled"}
	case errors.As(err, &limited):
		return &shellErr{codeRateLimited, "the instance is rate limiting this app; try again shortly"}
	case errors.As(err, &coded) && coded.Code == clienterr.ExitAuth:
		return &shellErr{codeFailed, "the sign-in expired or was not approved"}
	default:
		log.Printf("shell route failed: %s", logSafe(err.Error()))
		return &shellErr{codeFailed, fmt.Sprintf("the operation failed (%s)", shortErr(err))}
	}
}

func shortErr(err error) string {
	var coded *clienterr.Error
	if errors.As(err, &coded) {
		return fmt.Sprintf("exit %d", coded.Code)
	}
	return "unexpected error"
}

// logSafe bounds text that may come from the instance and strips control
// characters, so a hostile instance can't forge log lines.
func logSafe(s string) string {
	const limit = 200
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r) {
			return '?'
		}
		return r
	}, s)
	if len(s) > limit {
		s = strings.ToValidUTF8(s[:limit], "") + "..."
	}
	return s
}
