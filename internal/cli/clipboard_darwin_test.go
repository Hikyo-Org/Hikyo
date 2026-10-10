package cli

import (
	"os/exec"
	"strings"
	"testing"
)

// Exercise actual AppKit on a unique private pasteboard. Never inspect or
// overwrite the user's general pasteboard, even with synthetic test text.
func TestDarwinProtectedClipboardItemOnPrivatePasteboard(t *testing.T) {
	script := strings.Replace(protectedClipboardScript, "$.NSPasteboard.generalPasteboard", "$.NSPasteboard.pasteboardWithUniqueName", 1)
	script = strings.Replace(script, "if (!board.writeObjects($.NSArray.arrayWithObject(item))) throw new Error('Cannot write protected clipboard item');", `
  try {
    if (!board.writeObjects($.NSArray.arrayWithObject(item))) throw new Error('Cannot write protected clipboard item');
    if (ObjC.unwrap(board.stringForType($.NSPasteboardTypeString)) !== ObjC.unwrap(text))
      throw new Error('Text did not round trip');
    const types = ObjC.deepUnwrap(board.types);
    if (types.indexOf('org.nspasteboard.ConcealedType') < 0 ||
        types.indexOf('org.nspasteboard.TransientType') < 0)
      throw new Error('Exclusion markers missing');
  } finally { board.releaseGlobally; }`, 1)
	for _, value := range []string{"", "synthetic multiline\n雪\n\"'$(echo nope)\n"} {
		command := exec.CommandContext(t.Context(), "/usr/bin/osascript", "-l", "JavaScript", "-e", script)
		command.Stdin = strings.NewReader(value)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("private AppKit clipboard test: %v: %s", err, output)
		}
	}
}
