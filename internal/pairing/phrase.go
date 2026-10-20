// Package pairing implements the cryptographic adapters for manually authorized
// instance pairing. Invitation authorization and atomic replay consumption belong
// to the application transaction, not this package.
package pairing

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"unicode/utf8"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/sethvargo/go-diceware/diceware"
)

const (
	PhraseWords = 6
	// Six independent uniform selections from EFF's 7776-word list.
	PhraseEntropyBits = 77.54887502163469
	// Payload limits bound secret processing and encrypted continuation sizes.
	// They are not password-strength requirements.
	MaxPhraseBytes = 4096
	MaxPhraseWords = 128
)

// GeneratePhrase uses the library's CSPRNG dice rolls independently for each
// word. Repeated words are valid and do not trigger resampling.
func GeneratePhrase() (string, error) {
	return GeneratePhraseWords(PhraseWords)
}

// GeneratePhraseWords lets the requester choose the generated phrase length.
func GeneratePhraseWords(count int) (string, error) {
	if count < 1 || count > MaxPhraseWords {
		return "", errors.New("pairing: invalid generated word count")
	}
	generator, err := diceware.NewGenerator(nil)
	if err != nil {
		return "", err
	}
	list := diceware.WordListEffLarge()
	words := make([]string, count)
	for i := range words {
		index, err := generator.RollWord(list.Digits())
		if err != nil {
			return "", errors.New("pairing: phrase generation failed")
		}
		words[i] = list.WordAt(index)
	}
	return strings.Join(words, " "), nil
}

// ValidatePhrase accepts user-chosen passwords and phrases without a minimum
// strength or word count. Case, spacing and Unicode bytes remain significant.
func ValidatePhrase(raw string) error {
	if len(raw) == 0 || len(raw) > MaxPhraseBytes || !utf8.ValidString(raw) {
		return errors.New("pairing: invalid password or phrase")
	}
	return nil
}

func NewSigningKey() (ed25519.PrivateKey, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	return private, err
}

func PublicJWK(private ed25519.PrivateKey) (jose.JSONWebKey, error) {
	if len(private) != ed25519.PrivateKeySize {
		return jose.JSONWebKey{}, errors.New("pairing: invalid signing key")
	}
	return jose.JSONWebKey{Key: private.Public(), Algorithm: string(jose.EdDSA), Use: "sig"}, nil
}
