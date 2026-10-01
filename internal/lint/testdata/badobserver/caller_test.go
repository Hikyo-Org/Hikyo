package badobserver

import (
	"testing"

	hooks "github.com/Hikyo-Org/hikyo/internal/lint/testdata/badobserver/owner"
)

func TestAllowed(t *testing.T) { hooks.SetQueryObserver() }
