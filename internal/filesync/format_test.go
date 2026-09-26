package filesync

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/dotenv"
	"gopkg.in/yaml.v3"
)

func ptr(s string) *string { return &s }

// hostile values every format must carry byte-exact or refuse by name.
var hostile = map[string]string{
	"A_BOOL":   "on",
	"B_NUM":    "1e3",
	"C_NULL":   "null",
	"D_EMPTY":  "",
	"E_QUOTES": `he said "hi" and 'bye'`,
	"F_LINES":  "line1\nline2\r\n",
	"G_SPACE":  "  padded  ",
	"H_HASH":   "# not a comment",
	"I_DOLLAR": "$HOME ${X} \\n",
	"J_UNI":    "caf\u00e9 \u2028 \u0085 \ufeff \x01 \x7f",
	"NO":       "yes",
}

func hostileKeys() []string {
	var keys []string
	for k := range hostile {
		keys = append(keys, k)
	}
	return keys
}

func TestRenderGolden(t *testing.T) {
	t.Parallel()
	values := map[string]string{"B": "two words", "A": "x=1", "C": "on"}
	keys := []string{"B", "A", "C"}
	cases := map[Format]string{
		FormatDotenv: "B=two words\nA=x=1\nC=on\n",
		FormatJSON:   "{\n  \"A\": \"x=1\",\n  \"B\": \"two words\",\n  \"C\": \"on\"\n}\n",
		FormatYAML:   "\"A\": \"x=1\"\n\"B\": \"two words\"\n\"C\": \"on\"\n",
	}
	for format, want := range cases {
		got, refused := Render(format, keys, values)
		if len(refused) > 0 || string(got) != want {
			t.Errorf("%s = %q (%v), want %q", format, got, refused, want)
		}
	}
	got, refused := Render(FormatRaw, []string{"A"}, map[string]string{"A": "no newline added"})
	if len(refused) > 0 || string(got) != "no newline added" {
		t.Errorf("raw = %q", got)
	}
}

func TestRenderRoundTripsByteExact(t *testing.T) {
	t.Parallel()
	keys := hostileKeys()

	out, refused := Render(FormatDotenv, keys, hostile)
	if len(refused) > 0 {
		t.Fatal(refused)
	}
	entries, err := dotenv.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	gotDotenv := map[string]string{}
	for _, e := range entries {
		gotDotenv[e.Key] = e.Value
	}
	assertSame(t, "dotenv", gotDotenv)

	out, refused = Render(FormatJSON, keys, hostile)
	if len(refused) > 0 {
		t.Fatal(refused)
	}
	var gotJSON map[string]string
	if err := json.Unmarshal(out, &gotJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `<`) {
		t.Fatal("json escaped HTML")
	}
	assertSame(t, "json", gotJSON)

	out, refused = Render(FormatYAML, keys, hostile)
	if len(refused) > 0 {
		t.Fatal(refused)
	}
	// Decoding into interface{} proves no scalar resolved to a bool, number
	// or null: every value must come back a string.
	var gotYAML map[string]any
	if err := yaml.Unmarshal(out, &gotYAML); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	asStrings := map[string]string{}
	for k, v := range gotYAML {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("yaml %s decoded as %T", k, v)
		}
		asStrings[k] = s
	}
	assertSame(t, "yaml", asStrings)

	for k, v := range hostile {
		got, refused := Render(FormatRaw, []string{k}, hostile)
		if len(refused) > 0 || string(got) != v {
			t.Fatalf("raw %s = %q", k, got)
		}
	}
}

func assertSame(t *testing.T, format string, got map[string]string) {
	t.Helper()
	if len(got) != len(hostile) {
		t.Fatalf("%s: %d keys, want %d", format, len(got), len(hostile))
	}
	for k, want := range hostile {
		if got[k] != want {
			t.Fatalf("%s: %s = %q, want %q", format, k, got[k], want)
		}
	}
}

func TestRenderRefusesByName(t *testing.T) {
	t.Parallel()
	values := map[string]string{"OK": "fine", "NUL": "a\x00b", "BAD_UTF8": "\xff\xfe"}
	for _, format := range []Format{FormatJSON, FormatYAML} {
		_, refused := Render(format, []string{"OK", "NUL", "BAD_UTF8"}, values)
		if len(refused) != 2 || refused[0].Key != "NUL" || refused[1].Key != "BAD_UTF8" {
			t.Fatalf("%s refusals = %+v", format, refused)
		}
	}
	_, refused := Render(FormatDotenv, []string{"OK", "NUL"}, values)
	if len(refused) != 1 || refused[0].Key != "NUL" {
		t.Fatalf("dotenv refusals = %+v", refused)
	}
	// dotenv carries arbitrary bytes that are not NUL.
	if _, refused := Render(FormatDotenv, []string{"BAD_UTF8"}, values); len(refused) != 0 {
		t.Fatalf("dotenv refused non-UTF-8: %+v", refused)
	}
}

func TestRenderAllRefusesBeforeAnyFile(t *testing.T) {
	t.Parallel()
	cfg, err := ParseConfig([]byte(strings.Replace(validConfig, "keys: [DATABASE_URL, API_TOKEN]",
		"keys: [DATABASE_URL, API_TOKEN, PATH]", 1)))
	if err != nil {
		t.Fatal(err)
	}
	rows := []Row{
		{Name: "DATABASE_URL", Classification: "config", Value: ptr("postgres://x")},
		{Name: "API_TOKEN", Classification: "secret"}, // presence-only
		{Name: "PATH", Classification: "config", Value: ptr("/bin")},
		// DB_PASSWORD missing entirely
	}
	files, refused := RenderAll(cfg, rows)
	if files != nil {
		t.Fatal("rendered files despite refusals")
	}
	var got []string
	for _, r := range refused {
		got = append(got, r.File+":"+r.Key)
	}
	want := "app.env:API_TOKEN,db-password:DB_PASSWORD"
	if strings.Join(got, ",") != want {
		t.Fatalf("refusals = %v, want %s", refused, want)
	}

	rows[1].Value = ptr("t")
	rows = append(rows, Row{Name: "DB_PASSWORD", Classification: "secret", Value: ptr("p")})
	_, refused = RenderAll(cfg, rows)
	if len(refused) != 1 || refused[0].Key != "PATH" || !strings.Contains(refused[0].Reason, "loader-control") {
		t.Fatalf("loader-control refusal = %+v", refused)
	}
	cfg.Files[0].AcknowledgeLoaderControl = []string{"PATH"}
	files, refused = RenderAll(cfg, rows)
	if len(refused) != 0 || len(files) != 2 {
		t.Fatalf("acknowledged render = %v %v", files, refused)
	}
	if string(files[1].Content) != "p" {
		t.Fatalf("raw = %q", files[1].Content)
	}
}
