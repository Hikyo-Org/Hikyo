package cli

import (
	"bytes"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no private clipboard. This test is explicitly enabled only on an
// isolated Windows CI host, never on a contributor's ordinary desktop.
func TestWindowsClipboardCustodyNativeProtectedFormats(t *testing.T) {
	isolatedHostedRunner := os.Getenv("GITHUB_ACTIONS") == "true" && os.Getenv("RUNNER_ENVIRONMENT") == "github-hosted" && os.Getenv("RUNNER_OS") == "Windows"
	if os.Getenv("HIKYO_TEST_WINDOWS_CLIPBOARD") != "1" && !isolatedHostedRunner {
		t.Skip("requires a hosted Windows CI runner or isolated host opt-in via HIKYO_TEST_WINDOWS_CLIPBOARD=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, value := range []string{"", "synthetic multiline\n雪🔒\n\"'$(echo nope)\n"} {
		if err := writeWindowsClipboard(t.Context(), value); err != nil {
			t.Fatal(err)
		}
		if ok, _, _ := clipboardUser32.NewProc("OpenClipboard").Call(0); ok == 0 {
			t.Fatal("cannot inspect synthetic clipboard")
		}
		func() {
			defer clipboardUser32.NewProc("CloseClipboard").Call()
			defer clipboardUser32.NewProc("EmptyClipboard").Call()
			for _, name := range clipboardExclusionFormats {
				format, err := registerWindowsClipboardFormat(name)
				if err != nil {
					t.Fatal(err)
				}
				if got := readWindowsClipboardBytes(t, format, 4); !bytes.Equal(got, []byte{0, 0, 0, 0}) {
					t.Fatalf("exclusion %s is not false", name)
				}
			}
			text, err := windows.UTF16FromString(value)
			if err != nil {
				t.Fatal(err)
			}
			expected := unsafe.Slice((*byte)(unsafe.Pointer(&text[0])), len(text)*2)
			if got := readWindowsClipboardBytes(t, 13, len(expected)); !bytes.Equal(got, expected) {
				t.Fatal("synthetic UTF-16 text did not round trip")
			}
		}()
	}
}

func readWindowsClipboardBytes(t *testing.T, format uintptr, length int) []byte {
	t.Helper()
	handle, _, _ := clipboardUser32.NewProc("GetClipboardData").Call(format)
	if handle == 0 {
		t.Fatalf("clipboard format %d missing", format)
	}
	size, _, _ := clipboardKernel32.NewProc("GlobalSize").Call(handle)
	if size < uintptr(length) {
		t.Fatalf("clipboard format %d truncated", format)
	}
	address, _, _ := clipboardKernel32.NewProc("GlobalLock").Call(handle)
	if address == 0 {
		t.Fatalf("clipboard format %d cannot lock", format)
	}
	defer clipboardKernel32.NewProc("GlobalUnlock").Call(handle)
	data := make([]byte, length)
	clipboardNtdll.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&data[0])), address, uintptr(length))
	runtime.KeepAlive(data)
	return data
}
