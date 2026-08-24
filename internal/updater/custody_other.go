//go:build !unix

package updater

import "errors"

func validateConfigCustody(string) error {
	return errors.New("updater: the local Unix-socket helper is unsupported on this platform")
}
