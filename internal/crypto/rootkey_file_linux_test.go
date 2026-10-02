//go:build linux

package crypto

import (
	"fmt"
	"os"
	"testing"
)

func TestRootKeyFileReadsInheritedDescriptorRepeatedly(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "root-key")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	want := []byte(EncodeRootKey(make([]byte, KeySize)))
	if _, err := file.Write(want); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/proc/self/fd/%d", file.Fd())
	for attempt := 1; attempt <= 2; attempt++ {
		got, err := readRootKeyFile(path)
		if err != nil {
			t.Fatalf("read %d: %v", attempt, err)
		}
		if string(got) != string(want) {
			t.Fatalf("read %d returned different root key", attempt)
		}
	}
}
