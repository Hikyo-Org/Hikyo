package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"

	srp "github.com/ProtonMail/go-srp"
)

const (
	SRPVersion      = 4
	SRPBits         = 2048
	SRPElementBytes = SRPBits / 8
)

// This public safe-prime modulus and its signature are the upstream v0.0.7
// testModulusClearSign, not a deployment credential. The library verifies the
// signature with its embedded Proton modulus signing key. Neither invites nor
// network messages can select or replace these protocol parameters.
// https://github.com/ProtonMail/go-srp/blob/v0.0.7/srp_test.go
const signedModulus = `-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

W2z5HBi8RvsfYzZTS7qBaUxxPhsfHJFZpu3Kd6s1JafNrCCH9rfvPLrfuqocxWPgWDH2R8neK7PkNvjxto9TStuY5z7jAzWRvFWN9cQhAKkdWgy0JY6ywVn22+HFpF4cYesHrqFIKUPDMSSIlWjBVmEJZ/MusD44ZT29xcPrOqeZvwtCffKtGAIjLYPZIEbZKnDM1Dm3q2K/xS5h+xdhjnndhsrkwm9U9oyA2wxzSXFL+pdfj2fOdRwuR5nW0J2NFrq3kJjkRmpO/Genq1UW+TEknIWAb6VzJJJA244K/H8cnSx2+nSNZO3bbo6Ys228ruV9A8m6DhxmS+bihN3ttQ==
-----BEGIN PGP SIGNATURE-----
Version: ProtonMail
Comment: https://protonmail.com

wl4EARYIABAFAlwB1j0JEDUFhcTpUY8mAAD8CgEAnsFnF4cF0uSHKkXa1GIa
GO86yMV4zDZEZcDSJo0fgr8A/AlupGN9EdHlsrZLmTA1vhIx+rOgxdEff28N
kvNM7qIK
=q6vu
-----END PGP SIGNATURE-----`

var ErrInvalidExchange = errors.New("pairing: phrase authentication failed")

// Context binds the library's password KDF to this invitation and the
// requester's independently generated signing key. Both parties construct this
// from their own expected identities. Do not accept requester-controlled serving
// origin or pin values on the server.
type Context struct {
	InviteID            string `json:"invite_id"`
	Origin              string `json:"origin"`
	SPKIPin             string `json:"spki_pin"`
	RequesterThumbprint string `json:"requester_jkt"`
	InviteSalt          []byte `json:"invite_salt"`
}

type Enrollment struct {
	Version  int    `json:"version"`
	Salt     []byte `json:"salt"`
	Verifier []byte `json:"verifier"`
}

// ServerChallenge is serializable. Secret is sensitive ephemeral state and
// must be sealed in durable storage, never included in a response or logs.
type ServerChallenge struct {
	Secret    []byte `json:"secret"`
	Ephemeral []byte `json:"ephemeral"`
}

type Proofs struct {
	ClientEphemeral     []byte `json:"client_ephemeral"`
	ClientProof         []byte `json:"client_proof"`
	ExpectedServerProof []byte `json:"expected_server_proof"`
}

// ContextSalt supplies a 128-bit salt to the existing v4 password KDF. The
// library appends "proton" and its bcrypt dependency reads the first 16 salt
// bytes. Supplying 16 bytes therefore preserves all 128 context-binding bits.
// This application profile deliberately differs from Proton's usual 10-byte
// salt convention. It never modifies the PAKE arithmetic or proof construction.
func ContextSalt(context Context) ([]byte, error) {
	if context.InviteID == "" || len(context.InviteID) > 128 || len(context.InviteSalt) != 32 || len(context.Origin) > 2048 {
		return nil, ErrInvalidExchange
	}
	if _, err := proofTarget(context.Origin); err != nil {
		return nil, ErrInvalidExchange
	}
	pin, err := base64.StdEncoding.Strict().DecodeString(context.SPKIPin)
	if err != nil || len(pin) != sha256.Size {
		return nil, ErrInvalidExchange
	}
	jkt, err := base64.RawURLEncoding.Strict().DecodeString(context.RequesterThumbprint)
	if err != nil || len(jkt) != sha256.Size {
		return nil, ErrInvalidExchange
	}
	profile := struct {
		Domain  string  `json:"domain"`
		Context Context `json:"context"`
	}{Domain: "hikyo-instance-pairing-srp-v1", Context: context}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return nil, ErrInvalidExchange
	}
	hash := sha256.Sum256(encoded)
	return append([]byte(nil), hash[:16]...), nil
}

func EnrollPhrase(phrase string, context Context) (Enrollment, error) {
	if ValidatePhrase(phrase) != nil {
		return Enrollment{}, ErrInvalidExchange
	}
	salt, err := ContextSalt(context)
	if err != nil {
		return Enrollment{}, err
	}
	password := srpPassword(phrase, salt)
	defer clear(password)
	auth, err := srp.NewAuthForVerifier(password, signedModulus, salt)
	if err != nil {
		return Enrollment{}, ErrInvalidExchange
	}
	verifier, err := auth.GenerateVerifier(SRPBits)
	if err != nil {
		return Enrollment{}, ErrInvalidExchange
	}
	return Enrollment{Version: SRPVersion, Salt: salt, Verifier: verifier}, nil
}

func StartServer(enrollment Enrollment) (ServerChallenge, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return ServerChallenge{}, err
	}
	server, err := restoreServer(enrollment, secret)
	if err != nil {
		return ServerChallenge{}, err
	}
	challenge, err := server.GenerateChallenge()
	if err != nil {
		return ServerChallenge{}, ErrInvalidExchange
	}
	return ServerChallenge{Secret: secret, Ephemeral: challenge}, nil
}

func FinishServer(enrollment Enrollment, secret, clientEphemeral, clientProof []byte) ([]byte, error) {
	if len(clientEphemeral) != SRPElementBytes || len(clientProof) != SRPElementBytes {
		return nil, ErrInvalidExchange
	}
	server, err := restoreServer(enrollment, secret)
	if err != nil {
		return nil, err
	}
	if _, err := server.GenerateChallenge(); err != nil {
		return nil, ErrInvalidExchange
	}
	proof, err := server.VerifyProofs(clientEphemeral, clientProof)
	if err != nil {
		return nil, ErrInvalidExchange
	}
	return proof, nil
}

func ClientProof(phrase string, context Context, serverEphemeral []byte) (Proofs, error) {
	if len(serverEphemeral) != SRPElementBytes {
		return Proofs{}, ErrInvalidExchange
	}
	if ValidatePhrase(phrase) != nil {
		return Proofs{}, ErrInvalidExchange
	}
	salt, err := ContextSalt(context)
	if err != nil {
		return Proofs{}, err
	}
	password := srpPassword(phrase, salt)
	defer clear(password)
	auth, err := srp.NewAuth(SRPVersion, "", password, base64.StdEncoding.EncodeToString(salt), signedModulus, base64.StdEncoding.EncodeToString(serverEphemeral))
	if err != nil {
		return Proofs{}, ErrInvalidExchange
	}
	proof, err := auth.GenerateProofs(SRPBits)
	if err != nil {
		return Proofs{}, ErrInvalidExchange
	}
	return Proofs{ClientEphemeral: proof.ClientEphemeral, ClientProof: proof.ClientProof, ExpectedServerProof: proof.ExpectedServerProof}, nil
}

// Proton's bcrypt KDF consumes only 72 password bytes. A domain-separated,
// context-salted SHA-256 digest encodes the entire exact secret into 43 printable
// bytes before that existing slow KDF. It neither replaces bcrypt nor seeds the
// independent signing key. The public per-invitation salt is not a pepper.
func srpPassword(phrase string, salt []byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("hikyo.remote-pairing.password.v1\x00"))
	_, _ = hash.Write(salt)
	_, _ = hash.Write([]byte(phrase))
	digest := hash.Sum(nil)
	defer clear(digest)
	password := make([]byte, base64.RawURLEncoding.EncodedLen(len(digest)))
	base64.RawURLEncoding.Encode(password, digest)
	return password
}

func VerifyServerProof(expected, actual []byte) error {
	if len(expected) != SRPElementBytes || len(actual) != SRPElementBytes || subtle.ConstantTimeCompare(expected, actual) != 1 {
		return ErrInvalidExchange
	}
	return nil
}

func restoreServer(enrollment Enrollment, secret []byte) (*srp.Server, error) {
	if enrollment.Version != SRPVersion || len(enrollment.Salt) != 16 || len(enrollment.Verifier) != SRPElementBytes || len(secret) != 32 {
		return nil, ErrInvalidExchange
	}
	// The signed constructor verifies the pinned upstream modulus before its
	// raw bytes are passed to the library's deterministic state restoration.
	auth, err := srp.NewAuthForVerifier([]byte("parameter-validation"), signedModulus, enrollment.Salt)
	if err != nil {
		return nil, ErrInvalidExchange
	}
	server, err := srp.NewServerWithSecret(auth.Modulus, enrollment.Verifier, secret, SRPBits)
	if err != nil {
		return nil, ErrInvalidExchange
	}
	return server, nil
}
