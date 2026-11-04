package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

// openBrowser is the seam tests replace; production code never reassigns it.
var openBrowser = openExternal

type reauthRequest struct {
	Purpose        string   `json:"purpose"`
	Operation      string   `json:"operation"`
	EnvironmentIDs []string `json:"environment_ids"`
	KeyIDs         []string `json:"key_ids"`
}

// reauth ports internal/cli/cli_reauth.go: the passkey ceremony runs in the system
// browser, the shell redeems the result and keeps the rotated bearer. The page gets
// the redeemed windows without any token. Ceremonies are one at a time per connection.
func (c *conn) reauth(w http.ResponseWriter, r *http.Request) {
	if !c.busy.CompareAndSwap(false, true) {
		http.Error(w, "a ceremony is already running on this connection", http.StatusConflict)
		return
	}
	defer c.busy.Store(false)

	var in reauthRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || len(in.EnvironmentIDs) == 0 || len(in.KeyIDs) == 0 {
		http.Error(w, "a disclosure ceremony needs its environments and keys", http.StatusBadRequest)
		return
	}
	windows, err := c.runReauth(r.Context(), in)
	if err != nil {
		log.Printf("reauth failed: %v", err)
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		http.Error(w, "the system-browser ceremony failed", status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"windows": windows})
}

func (c *conn) runReauth(ctx context.Context, in reauthRequest) ([]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("binding the callback: %w", err)
	}
	defer listener.Close()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(verifier))
	var started struct {
		State string `json:"state"`
	}
	if err := c.post(ctx, "/api/v1/auth/cli-reauth/start", map[string]any{
		"purpose":         in.Purpose,
		"operation":       in.Operation,
		"environment_ids": in.EnvironmentIDs,
		"key_ids":         in.KeyIDs,
		"pkce_challenge":  base64.RawURLEncoding.EncodeToString(digest[:]),
		"redirect_uri":    "http://" + listener.Addr().String() + "/callback",
	}, &started); err != nil {
		return nil, err
	}
	if started.State == "" {
		return nil, errors.New("start returned no state")
	}

	code := make(chan string, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !exactCallbackQuery(r.URL.Query(), started.State) {
			http.Error(w, "This authorization callback did not match the request.", http.StatusBadRequest)
			return
		}
		accepted := false
		once.Do(func() { accepted = true; code <- r.URL.Query().Get("code") })
		if !accepted {
			http.Error(w, "This authorization callback was already used.", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Authorization received. Return to the Hikyo window.\n"))
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	target := *c.origin
	target.Path = "/reauth/cli"
	target.RawQuery = url.Values{"transaction": []string{started.State}}.Encode()
	if err := openBrowser(target.String()); err != nil {
		return nil, fmt.Errorf("opening the browser: %w", err)
	}

	select {
	case value := <-code:
		var redeemed struct {
			SessionToken string            `json:"session_token"`
			Windows      []json.RawMessage `json:"windows"`
		}
		if err := c.post(ctx, "/api/v1/auth/cli-reauth/redeem", map[string]string{"code": value, "pkce_verifier": verifier}, &redeemed); err != nil {
			return nil, err
		}
		if redeemed.SessionToken == "" {
			return nil, errors.New("redeem returned no rotated session token")
		}
		c.setBearer(redeemed.SessionToken)
		log.Printf("reauth: session rotated, token withheld from the page")
		// shortcut: the shell cannot take focus back from the browser (window.Focus and activateIgnoringOtherApps both failed), so the user returns to the window, which the reveal's own focus gate asks for; a product could show a notification.
		// A window result can carry its own session_token; the page never gets one.
		for i, win := range redeemed.Windows {
			redeemed.Windows[i], _ = takeRotatedToken(win)
		}
		return redeemed.Windows, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// post sends JSON with the connection's bearer and decodes a 2xx JSON reply.
func (c *conn) post(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin.JoinPath(path).String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.bearer())
	resp, err := bearerClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s -> %d %s", path, resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

func exactCallbackQuery(values url.Values, state string) bool {
	keys := slices.Sorted(maps.Keys(values))
	return slices.Equal(keys, []string{"code", "state"}) && len(values["code"]) == 1 && values.Get("code") != "" && len(values["state"]) == 1 && values.Get("state") == state
}
