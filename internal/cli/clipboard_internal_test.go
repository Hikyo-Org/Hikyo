package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestClipboardRefusesUnrevealedAndAbsentCells(t *testing.T) {
	secret := "must-not-copy"
	for _, cell := range []apigen.ValueCell{
		{Set: true, Value: &secret},
		{Revealed: true, Value: &secret},
		{Set: true, Revealed: true},
	} {
		err := copyCellToClipboard(t.Context(), IO{}, cell, func(context.Context, string) error {
			t.Fatal("unrevealed value reached clipboard")
			return nil
		})
		if err == nil {
			t.Fatal("unrevealed or absent value was accepted")
		}
	}
}

func TestClipboardPreservesMultilineAndEmptyValuesAndRedactsErrors(t *testing.T) {
	for _, value := range []string{"", "line 1\nline 2\n雪\"'$(echo nope)"} {
		var stdout, stderr bytes.Buffer
		ios := IO{Stdout: &stdout, Stderr: &stderr}
		cell := apigen.ValueCell{Set: true, Revealed: true, Value: &value}
		err := copyCellToClipboard(t.Context(), ios, cell, func(_ context.Context, got string) error {
			if got != value {
				t.Fatalf("copied %q, want %q", got, value)
			}
			return errors.New("error contains must-not-leak: " + value)
		})
		if err == nil || strings.Contains(err.Error(), "must-not-leak") || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("platform error leaked: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}
	}
}
