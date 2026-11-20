package webauthnrp

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// The persisted enrol session keeps the bytes encoding/json (v1) wrote; v2
// alone adds "largeBlobRead":false and three more false extension members.
func TestEnrolSessionKeepsV1Bytes(t *testing.T) {
	rp, err := FromExternalOrigin("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	_, session, challenge, err := rp.BeginEnrol(User{Handle: []byte("h"), Name: "a", DisplayName: "A"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Replace(string(session), `"challenge":"`+challenge+`"`, `"challenge":"C"`, 1)
	const want = `{"challenge":"C","rpId":"example.com","user_id":"aA==","expires":"0001-01-01T00:00:00Z","userVerification":"required","extensions":{"requested":["credProps"]},"credParams":[{"type":"public-key","alg":-7},{"type":"public-key","alg":-35},{"type":"public-key","alg":-36},{"type":"public-key","alg":-257},{"type":"public-key","alg":-258},{"type":"public-key","alg":-259},{"type":"public-key","alg":-37},{"type":"public-key","alg":-38},{"type":"public-key","alg":-39},{"type":"public-key","alg":-8}]}`
	if got != want {
		t.Fatalf("enrol session bytes changed:\n got %s\nwant %s", got, want)
	}
}

// Set flags keep their v1 position and the session still round-trips.
func TestSessionWithFlagsKeepsV1Bytes(t *testing.T) {
	sess := webauthn.SessionData{Challenge: "c", Expires: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC), Extensions: protocol.SessionExtensions{LargeBlobRead: true}, Mediation: protocol.MediationConditional, AuthorizeUVInitialization: true}
	got, err := json.Marshal(&sess, sessionJSON)
	const want = `{"challenge":"c","expires":"2026-11-13T12:00:00Z","extensions":{"largeBlobRead":true},"mediation":"conditional","authorizeUVInitialization":true}`
	if err != nil || string(got) != want {
		t.Fatalf("session bytes changed (%v):\n got %s\nwant %s", err, got, want)
	}
	var back webauthn.SessionData
	if err := json.Unmarshal(got, &back); err != nil || !back.AuthorizeUVInitialization || !back.Extensions.LargeBlobRead || !back.Expires.Equal(sess.Expires) {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}
