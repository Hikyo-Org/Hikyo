package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPasswordPolicy(t *testing.T) {
	cases := map[string]struct {
		password string
		want     error
	}{
		"below the floor":             {"short", ErrWeakPassword},
		"exactly at the floor":        {"twelvechars", ErrWeakPassword}, // 11
		"one character repeated":      {"aaaaaaaaaaaaaa", ErrWeakPassword},
		"on the list":                 {"qwerty123456", ErrCommonPassword},
		"on the list, different case": {"Qwerty123456", ErrCommonPassword},
		"fine":                        {"a perfectly ordinary passphrase", nil},
		// No composition rules: a long all-lowercase phrase with no digit,
		// symbol or capital is accepted, because demanding them produces
		// `Password1!` and a false sense of entropy.
		"no composition rules": {"whistling kettle on the hob", nil},
		// No forced rotation and no length ceiling that would push people
		// toward shorter secrets.
		"very long": {strings.Repeat("passphrase segment ", 20), nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := CheckPassword(tc.password)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("rejected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCommonListIsPinnedTop100KCorpus(t *testing.T) {
	const wantSHA256 = "c2e5696882c603b76bb67a47ee970897e5a76fc4c3f5547abe3d0ca340c576e0"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(commonPasswords))); got != wantSHA256 {
		t.Fatalf("common-password corpus SHA-256 = %s, want %s", got, wantSHA256)
	}
	n := len(commonList())
	if n < 97_000 {
		t.Fatalf("common-password corpus has %d effective entries, want at least 97,000", n)
	}
}
