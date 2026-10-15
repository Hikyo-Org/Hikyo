package webauthnrp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/webauthnrp"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

func TestFromExternalOriginRequiresExactHostnameOrigin(t *testing.T) {
	for _, origin := range []string{
		"", "localhost:8080", "https://:443", "http://127.0.0.1:8080", "https://192.0.2.1",
		"http://[::1]:8080", "https://[::]", "https://[::ffff:127.0.0.1]",
		"https://example.com/", "https://example.com/path", "https://example.com?token=secret",
		"https://example.com?", "https://example.com#fragment", "https://example.com#",
		"https://user:password@example.com", "ftp://example.com",
	} {
		t.Run(origin, func(t *testing.T) {
			if _, err := webauthnrp.FromExternalOrigin(origin); !errors.Is(err, webauthnrp.ErrConfig) {
				t.Fatalf("FromExternalOrigin(%q) = %v, want ErrConfig", origin, err)
			}
		})
	}
	for _, origin := range []string{"http://localhost:8080", "https://example.com", "https://example.com:9443"} {
		t.Run(origin, func(t *testing.T) {
			rp, err := webauthnrp.FromExternalOrigin(origin)
			if err != nil {
				t.Fatal(err)
			}
			host := strings.Split(strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://"), ":")[0]
			user := webauthnrp.User{Handle: []byte("account-handle"), Name: "alice", DisplayName: "Alice"}
			options, session, _, err := rp.BeginEnrol(user)
			if err != nil {
				t.Fatal(err)
			}
			device := webauthntest.New(host, origin)
			response, err := device.Enrol(options)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rp.FinishEnrol(user, session, response); err != nil {
				t.Fatalf("hostname origin ceremony: %v", err)
			}
		})
	}
}

func TestRegistrationThenDiscoverableLogin(t *testing.T) {
	rp, err := webauthnrp.New("example.com", []string{"https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	user := webauthnrp.User{Handle: []byte("account-handle"), Name: "alice", DisplayName: "Alice"}
	device := webauthntest.New("example.com", "https://example.com")
	options, session, _, err := rp.BeginEnrol(user)
	if err != nil {
		t.Fatal(err)
	}
	response, err := device.Enrol(options)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := rp.FinishEnrol(user, session, response)
	if err != nil {
		t.Fatal(err)
	}
	user.Credentials = []webauthnrp.Credential{{ID: registration.CredentialID, PublicKey: registration.PublicKey, AAGUID: registration.AAGUID, SignCount: registration.SignCount}}
	options, session, _, err = rp.BeginDiscoverableLogin()
	if err != nil {
		t.Fatal(err)
	}
	response, err = device.Assert(options)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(id, handle []byte) (webauthnrp.User, error) {
		if !bytes.Equal(id, registration.CredentialID) || !bytes.Equal(handle, user.Handle) {
			return webauthnrp.User{}, errors.New("credential identity mismatch")
		}
		return user, nil
	}
	if _, err := rp.FinishDiscoverableLogin(session, response, lookup); err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(response, &wire); err != nil {
		t.Fatal(err)
	}
	wire["clientExtensionResults"] = json.RawMessage(`{"credProps":{"rk":true}}`)
	response, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rp.FinishDiscoverableLogin(session, response, lookup); !errors.Is(err, webauthnrp.ErrResponse) {
		t.Fatalf("unsolicited registration extension accepted during login: %v", err)
	}
}

func TestRegistrationCredentialProperties(t *testing.T) {
	for _, tc := range []struct {
		name       string
		extensions string
		present    bool
		resident   bool
		refused    bool
	}{
		{name: "discoverable", extensions: `{"credProps":{"rk":true}}`, present: true, resident: true},
		{name: "not discoverable", extensions: `{"credProps":{"rk":false}}`, present: true},
		{name: "extension absent", extensions: `{}`},
		{name: "residency absent", extensions: `{"credProps":{}}`},
		{name: "malformed residency", extensions: `{"credProps":{"rk":"true"}}`, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rp, err := webauthnrp.New("example.com", []string{"https://example.com"})
			if err != nil {
				t.Fatal(err)
			}
			user := webauthnrp.User{Handle: []byte("account-handle"), Name: "alice", DisplayName: "Alice"}
			options, session, _, err := rp.BeginEnrol(user)
			if err != nil {
				t.Fatal(err)
			}
			device := webauthntest.New("example.com", "https://example.com")
			response, err := device.Enrol(options)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(response, &wire); err != nil {
				t.Fatal(err)
			}
			// The software authenticator reports residency only when requested.
			// Assert that real request/response path before varying the output.
			var requested struct {
				CredProps struct {
					RK bool `json:"rk"`
				} `json:"credProps"`
			}
			if err := json.Unmarshal(wire["clientExtensionResults"], &requested); err != nil || !requested.CredProps.RK {
				t.Fatalf("registration did not request credential properties: %s (%v)", response, err)
			}
			wire["clientExtensionResults"] = json.RawMessage(tc.extensions)
			response, err = json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			registration, err := rp.FinishEnrol(user, session, response)
			if tc.refused {
				if !errors.Is(err, webauthnrp.ErrResponse) {
					t.Fatalf("malformed extension accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			resident, present := registration.CredProps["rk"]
			if present != tc.present || (present && resident != tc.resident) {
				t.Fatalf("credential properties = %#v, want present=%t resident=%t", registration.CredProps, tc.present, tc.resident)
			}
		})
	}
}
