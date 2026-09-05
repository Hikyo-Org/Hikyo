//go:build !windows

package backupreceipt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type replaceContext struct {
	context.Context
	parent, renamed string
	t               *testing.T
}

func (c *replaceContext) Err() error {
	if c.renamed != "" {
		return context.Canceled
	}
	entries, err := os.ReadDir(c.parent)
	if err != nil {
		c.t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".hikyo-upgrade-evidence-") {
			original := filepath.Join(c.parent, entry.Name())
			c.renamed = original
			if err := os.Rename(original, original+"-held"); err != nil {
				c.t.Fatal(err)
			}
			if err := os.Mkdir(original, 0700); err != nil {
				c.t.Fatal(err)
			}
			return context.Canceled
		}
	}
	return nil
}
func TestCiphertextFailedCopyPreservesReplacement(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.age")
	if err := os.WriteFile(source, []byte("ciphertext"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := &replaceContext{Context: context.Background(), parent: t.TempDir(), t: t}
	if _, err := PinCiphertext(ctx, source, ctx.parent); err == nil {
		t.Fatal("injected cancellation ignored")
	}
	if ctx.renamed == "" {
		t.Fatal("replacement trigger missed")
	}
	if _, err := os.Stat(ctx.renamed); err != nil {
		t.Fatal("failed copy deleted unrelated replacement directory", err)
	}
}
