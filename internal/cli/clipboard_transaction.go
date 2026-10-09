package cli

import (
	"context"
	"errors"
)

var errClipboardValueMayRemain = errors.New("clipboard cleanup failed; the value may remain available to paste")

var clipboardExclusionFormats = [...]string{
	"ExcludeClipboardContentFromMonitorProcessing",
	"CanIncludeInClipboardHistory",
	"CanUploadToCloudClipboard",
}

// clipboardTransactionOps keeps allocation and publication separate. A prepared
// handle remains ours until set succeeds; Windows then owns and frees it.
type clipboardTransactionOps struct {
	register func(string) (uintptr, error)
	prepare  func([]byte) (uintptr, error)
	free     func(uintptr)
	open     func() error
	empty    func() error
	set      func(uintptr, uintptr) error
	close    func() error
}

func writeProtectedClipboardTransaction(ctx context.Context, text []byte, ops clipboardTransactionOps) (returnErr error) {
	formats := [4]uintptr{0, 0, 0, 13} // CF_UNICODETEXT is published last.
	var handles [4]uintptr
	defer func() {
		for _, handle := range handles {
			if handle != 0 {
				ops.free(handle)
			}
		}
	}()
	// Complete every fallible preparation before destroying the previous copy.
	for i, name := range clipboardExclusionFormats {
		format, err := ops.register(name)
		if err != nil {
			return err
		}
		formats[i] = format
	}
	for i := range handles {
		data := []byte{0, 0, 0, 0} // false DWORD for history/cloud eligibility.
		if i == len(handles)-1 {
			data = text
		}
		handle, err := ops.prepare(data)
		if err != nil {
			return err
		}
		handles[i] = handle
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ops.open(); err != nil {
		return err
	}
	changed, textPublished := false, false
	// One cleanup scope handles both publication and CloseClipboard failures.
	// Close failure must trigger rollback before the final release attempt.
	defer func() {
		if returnErr != nil && changed {
			if err := ops.empty(); err == nil {
				textPublished = false
			}
		}
		if err := ops.close(); err != nil {
			if textPublished {
				if rollbackErr := ops.empty(); rollbackErr == nil {
					textPublished = false
				}
			}
			ops.close() // Best effort release after rollback, with no data writes.
			if returnErr == nil {
				returnErr = err
			}
		}
		if returnErr != nil && textPublished {
			returnErr = errClipboardValueMayRemain
		}
	}()
	if err := ops.empty(); err != nil {
		return err
	}
	changed = true
	for i, handle := range handles {
		if err := ops.set(formats[i], handle); err != nil {
			return err
		}
		handles[i] = 0 // Successful set transfers allocation ownership.
		textPublished = i == len(handles)-1
	}
	return nil
}
