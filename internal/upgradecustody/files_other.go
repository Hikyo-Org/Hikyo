//go:build !unix

package upgradecustody

import (
	"errors"
	"os"
)

func custodyDirectory(string, bool, int) (*os.File, error) {
	return nil, errors.New("local operator custody is unsupported on this platform")
}
func publish(*os.File, []byte) error {
	return errors.New("local operator custody is unsupported on this platform")
}
func read(*os.File, int) ([]byte, error) {
	return nil, errors.New("local operator custody is unsupported on this platform")
}
