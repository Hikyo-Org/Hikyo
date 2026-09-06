//go:build !linux && !darwin

package upgradegate

import (
	"errors"
	"os"
)

func InspectCustodyDirectory(string) (os.FileInfo, error) {
	return nil, errors.New("upgrade custody inspection unsupported")
}
func inspectOperatorCustody(string, os.FileInfo) (operatorCustody, []byte, error) {
	return operatorCustody{}, nil, errors.New("upgrade custody inspection unsupported")
}
