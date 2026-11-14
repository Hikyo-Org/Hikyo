package compose

import (
	"encoding/json/v2"
	"os"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

func osMkdir(p string, perm os.FileMode) error { return os.Mkdir(p, perm) }

func osReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

func removeFile(p string) error { return os.Remove(p) }

func osWriteFile(p string, b []byte, perm os.FileMode) error { return os.WriteFile(p, b, perm) }

func osSymlink(oldname, newname string) error { return os.Symlink(oldname, newname) }

// hex32 returns 32 lowercase hex chars, a syntactically valid stamp body.
func hex32() string { return strings.Repeat("ab", 16) }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v, jsonwire.Stable)
	if err != nil {
		panic(err)
	}
	return b
}
