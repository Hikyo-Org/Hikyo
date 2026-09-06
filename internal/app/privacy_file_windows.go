//go:build windows

package app

import "errors"

func readPrivacyReceipt(path string) ([]byte, error) {
	return nil, errors.New("privacy: receipt replay requires an owner-only file check; run on the Unix server host")
}
