//go:build windows

package backupreceipt

import (
	"errors"
	"os"
)

func openCiphertextSource(string) (*os.File, error) {
	return nil, errors.New("upgrade custody drill requires a supported Unix operator host")
}

func openPinnedCiphertext(_ *os.Root) (*os.File, error) {
	return nil, errors.New("upgrade custody drill requires a supported Unix operator host")
}

func openOwnedReadonly(_ *os.Root, _ string) (*os.File, error) {
	return nil, errors.New("upgrade custody drill requires a supported Unix operator host")
}
