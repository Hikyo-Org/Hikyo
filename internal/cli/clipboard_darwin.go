package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// A fixed script and stdin keep plaintext out of argv, shell history, and
// temporary files. AppKit is supplied by macOS, preserving CGO_ENABLED=0.
// Publish one item containing both text and markers so observers cannot see an
// intermediate unmarked text item. The concealed convention is documented at
// https://nspasteboard.org/; transient asks managers to skip transient content.
const protectedClipboardScript = `
ObjC.import('AppKit');
ObjC.import('Foundation');
function run() {
  const data = $.NSFileHandle.fileHandleWithStandardInput.readDataToEndOfFile;
  const text = $.NSString.alloc.initWithDataEncoding(data, $.NSUTF8StringEncoding);
  if (!text) throw new Error('Invalid clipboard encoding');
  const item = $.NSPasteboardItem.alloc.init;
  if (!item.setStringForType('', 'org.nspasteboard.ConcealedType') ||
      !item.setStringForType('', 'org.nspasteboard.TransientType') ||
      !item.setStringForType(text, $.NSPasteboardTypeString)) {
    throw new Error('Cannot prepare protected clipboard item');
  }
  const board = $.NSPasteboard.generalPasteboard;
  board.clearContents;
  if (!board.writeObjects($.NSArray.arrayWithObject(item))) throw new Error('Cannot write protected clipboard item');
}
`

func prepareSecureClipboard() (func(context.Context, string) error, error) {
	if _, err := os.Stat("/usr/bin/osascript"); err != nil {
		return nil, err
	}
	return func(ctx context.Context, value string) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", protectedClipboardScript)
		command.Stdin = strings.NewReader(value)
		// Do not attach output streams or include subprocess diagnostics: an
		// interpreter error must never echo secret material into CLI output.
		if err := command.Run(); err != nil {
			return fmt.Errorf("protected clipboard write failed")
		}
		return nil
	}, nil
}
