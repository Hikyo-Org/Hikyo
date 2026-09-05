//go:build !linux && !darwin

package upgradegate

import (
	"context"
	"errors"
)

type operatorFile struct{ value operatorCustody }

func openOperatorFile(context.Context, string, []byte, bool) (*operatorFile, error) {
	return nil, errors.New("production operator custody requires Linux or macOS filesystem protections")
}
func (*operatorFile) save() error { return errors.New("operator custody unavailable") }
func (*operatorFile) close()      {}
