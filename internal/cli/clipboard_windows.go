package cli

import (
	"context"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native Win32 APIs preserve CGO_ENABLED=0 release builds. History formats:
// https://learn.microsoft.com/en-us/windows/win32/dataxchg/clipboard-formats
var clipboardUser32 = windows.NewLazySystemDLL("user32.dll")
var clipboardKernel32 = windows.NewLazySystemDLL("kernel32.dll")
var clipboardNtdll = windows.NewLazySystemDLL("ntdll.dll")

func prepareSecureClipboard() (func(context.Context, string) error, error) {
	return writeWindowsClipboard, nil
}

func writeWindowsClipboard(ctx context.Context, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	text, err := windows.UTF16FromString(value)
	if err != nil { // Embedded NUL cannot be represented without truncation.
		return errors.New("clipboard text contains an embedded NUL")
	}
	defer clear(text)
	// The HWND and clipboard ownership must remain on their creating thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	// A message-only window supplies a real owner. OpenClipboard(NULL) followed
	// by EmptyClipboard makes SetClipboardData fail according to Win32 docs.
	hwnd, _, _ := clipboardUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
	if hwnd == 0 {
		return errors.New("cannot create clipboard owner")
	}
	defer clipboardUser32.NewProc("DestroyWindow").Call(hwnd)
	data := unsafe.Slice((*byte)(unsafe.Pointer(&text[0])), len(text)*2)
	return writeProtectedClipboardTransaction(ctx, data, clipboardTransactionOps{
		register: registerWindowsClipboardFormat,
		prepare:  prepareWindowsClipboardData,
		free:     func(handle uintptr) { clipboardKernel32.NewProc("GlobalFree").Call(handle) },
		open: func() error {
			if ok, _, _ := clipboardUser32.NewProc("OpenClipboard").Call(hwnd); ok == 0 {
				return errors.New("clipboard is unavailable or busy")
			}
			return nil
		},
		empty: func() error {
			if ok, _, _ := clipboardUser32.NewProc("EmptyClipboard").Call(); ok == 0 {
				return errors.New("cannot empty clipboard")
			}
			return nil
		},
		set: func(format, handle uintptr) error {
			if result, _, _ := clipboardUser32.NewProc("SetClipboardData").Call(format, handle); result == 0 {
				return errors.New("cannot set protected clipboard data")
			}
			return nil
		},
		close: func() error {
			if ok, _, _ := clipboardUser32.NewProc("CloseClipboard").Call(); ok == 0 {
				return errors.New("cannot close clipboard")
			}
			return nil
		},
	})
}

func registerWindowsClipboardFormat(name string) (uintptr, error) {
	encoded, _ := windows.UTF16PtrFromString(name)
	format, _, _ := clipboardUser32.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(encoded)))
	if format == 0 {
		return 0, errors.New("cannot register clipboard exclusion")
	}
	return format, nil
}

func prepareWindowsClipboardData(data []byte) (uintptr, error) {
	memory, _, _ := clipboardKernel32.NewProc("GlobalAlloc").Call(0x0002, uintptr(len(data))) // GMEM_MOVEABLE
	if memory == 0 {
		return 0, errors.New("cannot allocate clipboard data")
	}
	owned := true
	defer func() {
		if owned {
			clipboardKernel32.NewProc("GlobalFree").Call(memory)
		}
	}()
	address, _, _ := clipboardKernel32.NewProc("GlobalLock").Call(memory)
	if address == 0 {
		return 0, errors.New("cannot lock clipboard data")
	}
	// Copy through the native API rather than turning a Win32 address into
	// a Go pointer; GlobalLock owns the destination allocation.
	clipboardNtdll.NewProc("RtlMoveMemory").Call(address, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	runtime.KeepAlive(data)
	if stillLocked, _, unlockErr := clipboardKernel32.NewProc("GlobalUnlock").Call(memory); stillLocked != 0 || unlockErr != windows.ERROR_SUCCESS {
		return 0, errors.New("cannot unlock clipboard data")
	}
	owned = false // The transaction owns this allocation until publication.
	return memory, nil
}
