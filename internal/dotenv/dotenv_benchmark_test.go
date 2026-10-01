package dotenv

import (
	"fmt"
	"strings"
	"testing"
)

// benchDocument builds a representative .env document mixing bare, quoted,
// escaped, exported and commented lines.
func benchDocument(n int) []byte {
	var b strings.Builder
	for i := range n {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "APP_SETTING_%d=value-%d\n", i, i)
		case 1:
			fmt.Fprintf(&b, "export SERVICE_URL_%d=https://svc-%d.example.com/path?q=%d\n", i, i, i)
		case 2:
			fmt.Fprintf(&b, "# comment for entry %d\nQUOTED_%d=\"line one\\nline two %d\"\n", i, i, i)
		default:
			fmt.Fprintf(&b, "SINGLE_%d='  padded # value %d  '\n", i, i)
		}
	}
	return []byte(b.String())
}

func BenchmarkParse(b *testing.B) {
	data := benchDocument(500)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	entries, err := Parse(benchDocument(500))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, refusals, err := Encode(entries); err != nil || len(refusals) > 0 {
			b.Fatalf("Encode: refusals=%v err=%v", refusals, err)
		}
	}
}
