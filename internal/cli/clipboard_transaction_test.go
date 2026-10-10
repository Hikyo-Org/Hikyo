package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

type clipboardTransactionFixture struct {
	registered, prepared, opened, emptied, closed int
	failRegister, failPrepare, failSet            int
	failOpen, failClose, failRollback             bool
	previous, text                                bool
	buffers                                       map[uintptr][]byte
	freed, transferred, formats                   []uintptr
}

func (f *clipboardTransactionFixture) ops(t *testing.T) clipboardTransactionOps {
	t.Helper()
	f.buffers = make(map[uintptr][]byte)
	return clipboardTransactionOps{
		register: func(name string) (uintptr, error) {
			f.registered++
			if name != clipboardExclusionFormats[f.registered-1] {
				t.Fatalf("unexpected exclusion %q", name)
			}
			if f.failRegister == f.registered {
				return 0, errors.New("registration failed")
			}
			return uintptr(100 + f.registered), nil
		},
		prepare: func(data []byte) (uintptr, error) {
			f.prepared++
			if f.failPrepare == f.prepared {
				return 0, errors.New("allocation or lock failed")
			}
			handle := uintptr(f.prepared)
			f.buffers[handle] = bytes.Clone(data)
			return handle, nil
		},
		free: func(handle uintptr) {
			if slices.Contains(f.transferred, handle) {
				t.Fatal("freed a Windows-owned allocation")
			}
			if slices.Contains(f.freed, handle) {
				t.Fatal("freed an allocation twice")
			}
			f.freed = append(f.freed, handle)
		},
		open: func() error {
			f.opened++
			if f.registered != 3 || f.prepared != 4 {
				t.Fatal("clipboard opened before all data prepared")
			}
			if f.failOpen {
				return errors.New("busy")
			}
			return nil
		},
		empty: func() error {
			f.emptied++
			if f.emptied > 1 && f.failRollback {
				return errors.New("rollback failed")
			}
			f.previous, f.text = false, false
			return nil
		},
		set: func(format, handle uintptr) error {
			f.formats = append(f.formats, format)
			if len(f.formats) == f.failSet {
				return errors.New("set failed")
			}
			f.transferred = append(f.transferred, handle)
			if format == 13 {
				if len(f.transferred) != 4 {
					t.Fatal("plaintext published without all exclusions")
				}
				f.text = true
			} else if !bytes.Equal(f.buffers[handle], []byte{0, 0, 0, 0}) {
				t.Fatal("nonzero exclusion DWORD")
			}
			return nil
		},
		close: func() error {
			f.closed++
			if f.closed == 1 && f.failClose {
				return errors.New("close failed")
			}
			return nil
		},
	}
}

func TestClipboardPreparationFailurePreservesPreviousCopy(t *testing.T) {
	for stage := 1; stage <= 7; stage++ {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			f := &clipboardTransactionFixture{previous: true}
			if stage <= 3 {
				f.failRegister = stage
			} else {
				f.failPrepare = stage - 3
			}
			if err := writeProtectedClipboardTransaction(t.Context(), []byte("secret"), f.ops(t)); err == nil {
				t.Fatal("failure accepted")
			}
			if !f.previous || f.opened != 0 || f.emptied != 0 {
				t.Fatal("preparation destroyed previous copy")
			}
			if len(f.freed) != len(f.buffers) {
				t.Fatal("untransferred allocation leaked")
			}
		})
	}
}

func TestClipboardPublicationFailureNeverPublishesText(t *testing.T) {
	for stage := 1; stage <= 4; stage++ {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			f := &clipboardTransactionFixture{failSet: stage}
			if err := writeProtectedClipboardTransaction(t.Context(), []byte("secret"), f.ops(t)); err == nil {
				t.Fatal("failure accepted")
			}
			if f.text || f.emptied != 2 || f.closed != 1 {
				t.Fatalf("unexpected cleanup: %+v", f)
			}
			if len(f.freed)+len(f.transferred) != 4 {
				t.Fatal("allocation ownership lost")
			}
		})
	}
}

func TestClipboardCloseFailureRollsBackOrWarns(t *testing.T) {
	for _, failRollback := range []bool{false, true} {
		f := &clipboardTransactionFixture{failClose: true, failRollback: failRollback}
		err := writeProtectedClipboardTransaction(t.Context(), []byte("secret"), f.ops(t))
		if err == nil || f.emptied != 2 || f.closed != 2 || f.text != failRollback || errors.Is(err, errClipboardValueMayRemain) != failRollback {
			t.Fatalf("rollback=%v err=%v fixture=%+v", failRollback, err, f)
		}
		if len(f.freed) != 0 {
			t.Fatal("freed transferred allocation")
		}
	}
}

func TestClipboardTransactionCancellationAndBusyPreservePreviousCopy(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		f := &clipboardTransactionFixture{previous: true, failOpen: !canceled}
		ctx, cancel := context.WithCancel(t.Context())
		if canceled {
			cancel()
		}
		err := writeProtectedClipboardTransaction(ctx, []byte("secret"), f.ops(t))
		cancel()
		if err == nil || !f.previous || f.emptied != 0 || len(f.freed) != 4 {
			t.Fatalf("unexpected failure handling: %v %+v", err, f)
		}
	}
}

func TestClipboardTransactionSuccessTransfersAllPreparedData(t *testing.T) {
	f := &clipboardTransactionFixture{previous: true}
	secret := []byte("secret")
	if err := writeProtectedClipboardTransaction(t.Context(), secret, f.ops(t)); err != nil {
		t.Fatal(err)
	}
	if !f.text || f.previous || f.emptied != 1 || f.closed != 1 || len(f.freed) != 0 || !slices.Equal(f.formats, []uintptr{101, 102, 103, 13}) || !bytes.Equal(f.buffers[4], secret) {
		t.Fatalf("unexpected transaction: %+v", f)
	}
}

func TestClipboardCleanupWarningDoesNotLeakValue(t *testing.T) {
	secret := "must-not-leak"
	var stdout, stderr bytes.Buffer
	err := copyCellToClipboard(t.Context(), IO{Stdout: &stdout, Stderr: &stderr}, apigen.ValueCell{Set: true, Revealed: true, Value: &secret}, func(context.Context, string) error {
		return fmt.Errorf("%w: %s", errClipboardValueMayRemain, secret)
	})
	if err == nil || !strings.Contains(err.Error(), "may remain available to paste") || strings.Contains(err.Error(), secret) || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("unsafe warning: %v", err)
	}
}
