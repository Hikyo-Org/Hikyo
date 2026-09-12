// registryfixture creates disposable credentials for the private-registry
// operator acceptance test. It never prints credential material.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: registryfixture OUTPUT_DIRECTORY")
	}
	password := make([]byte, 32)
	if _, err := rand.Read(password); err != nil {
		panic(err)
	}
	encoded := []byte(hex.EncodeToString(password))
	hash, err := crypto.RegistryPasswordHash(encoded)
	if err != nil {
		panic(err)
	}
	for name, content := range map[string][]byte{"password": encoded, "htpasswd": []byte(fmt.Sprintf("hikyo-test:%s\n", hash))} {
		if err := os.WriteFile(filepath.Join(os.Args[1], name), content, 0600); err != nil {
			panic(err)
		}
	}
}
