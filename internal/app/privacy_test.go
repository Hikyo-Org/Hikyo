package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivacyCLIRefusesBeforeBoot(t *testing.T) {
	for _, args := range [][]string{
		{}, {"bad"}, {"erase", "--principal", "usr_1", "--output-file", "unused"},
		{"export", "--principal", "usr_1"}, {"export", "--principal", "usr_1", "--output-file", "unused", "stray"},
		{"reapply", "--receipt", "unused", "--principal", "usr_1", "--output-file", "unused", "--confirm"},
	} {
		if err := runAdminPrivacy(context.Background(), nil, nil, args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing")
	if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runAdminPrivacy(context.Background(), nil, nil, []string{"erase", "--principal", "usr_1", "--output-file", path, "--confirm"}, io.Discard); err == nil {
		t.Fatal("existing receipt accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "preserve" {
		t.Fatal("existing receipt changed")
	}
}
func TestPrivacyReceiptFileBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix owner checks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivacyReceipt(path); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivacyReceipt(link); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivacyReceipt(path); err == nil {
		t.Fatal("readable-by-others receipt accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 4097)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivacyReceipt(path); err == nil {
		t.Fatal("oversized receipt accepted")
	}
}

func TestPrivacyReceiptClosedSchema(t *testing.T) {
	for _, raw := range []string{`{}`, `[]`, `{"version":1,"version":2}`, `{"unexpected":true}`, `{} {}`, `null`} {
		if _, err := parsePrivacyReceipt([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	raw := `{"version":1,"principal_id":"usr_1","account_id":"acc_1","instance_id":"ins_1","action":"erase","applied_at":"2026-09-05T00:00:00Z"}`
	if _, err := parsePrivacyReceipt([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}
