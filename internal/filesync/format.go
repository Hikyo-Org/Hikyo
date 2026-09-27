package filesync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/dotenv"
)

// Row is one delivered key as the renderer sees it. Value is nil when no
// plaintext crossed: a secret delivered presence-only, or a key the snapshot
// does not set.
type Row struct {
	KeyID          string
	Name           string
	Classification string
	Value          *string
}

// Refusal names one key a file cannot represent and why. A non-empty refusal
// list means nothing is written: refusals are reported before any temporary
// file exists.
type Refusal struct {
	File   string
	Key    string
	Reason string
}

func (r Refusal) String() string {
	return fmt.Sprintf("%s: %s: %s", r.File, r.Key, r.Reason)
}

// Rendered is one file's final bytes.
type Rendered struct {
	Name    string
	Content []byte
}

// RenderAll renders every configured file from one delivery. It either returns
// every file or refusals naming each unrepresentable key; it never returns a
// partial set.
func RenderAll(cfg *Config, rows []Row) ([]Rendered, []Refusal) {
	byName := make(map[string]Row, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}
	var out []Rendered
	var refusals []Refusal
	for _, f := range cfg.Files {
		values, missing := selectValues(f, byName)
		refusals = append(refusals, missing...)
		if len(missing) > 0 {
			continue
		}
		content, refused := Render(f.Format, f.Keys, values)
		for _, r := range refused {
			r.File = f.Name
			refusals = append(refusals, r)
		}
		if f.Format == FormatDotenv {
			refusals = append(refusals, loaderControlRefusals(f)...)
		}
		if len(refused) == 0 {
			out = append(out, Rendered{Name: f.Name, Content: content})
		}
	}
	if len(refusals) > 0 {
		return nil, refusals
	}
	return out, nil
}

// selectValues resolves a file's keys against the delivery, refusing by name
// every key that did not arrive with a value.
func selectValues(f File, byName map[string]Row) (map[string]string, []Refusal) {
	values := make(map[string]string, len(f.Keys))
	var refusals []Refusal
	for _, k := range f.Keys {
		row, ok := byName[k]
		switch {
		case !ok:
			refusals = append(refusals, Refusal{File: f.Name, Key: k, Reason: "not delivered by this target (deleted, renamed, or outside its key selection)"})
		case row.Value == nil && row.Classification == "secret":
			refusals = append(refusals, Refusal{File: f.Name, Key: k, Reason: "secret delivered presence-only; the target's identity needs `reveal` under the project's machine-reveal opt-in"})
		case row.Value == nil:
			refusals = append(refusals, Refusal{File: f.Name, Key: k, Reason: "has no value in this environment"})
		default:
			values[k] = *row.Value
		}
	}
	return values, refusals
}

// loaderControlRefusals applies the Compose rule to dotenv files, which are
// routinely loaded into a process environment: a loader-control name renders
// only when acknowledged by name.
func loaderControlRefusals(f File) []Refusal {
	var mapped []string
	for _, k := range f.Keys {
		if delivery.IsLoaderControlKey(k) {
			mapped = append(mapped, k)
		}
	}
	refused, _ := delivery.Unacknowledged(mapped, f.AcknowledgeLoaderControl)
	out := make([]Refusal, 0, len(refused))
	for _, k := range refused {
		out = append(out, Refusal{File: f.Name, Key: k, Reason: "loader-control key; list it under acknowledge_loader_control to render it"})
	}
	return out
}

// Render encodes one file. keys fixes the dotenv line order; json and yaml
// sort by key. Every refusal is returned (File left empty for the caller).
func Render(format Format, keys []string, values map[string]string) ([]byte, []Refusal) {
	switch format {
	case FormatDotenv:
		return renderDotenv(keys, values)
	case FormatJSON:
		return renderJSON(keys, values)
	case FormatYAML:
		return renderYAML(keys, values)
	case FormatRaw:
		return renderRaw(keys, values)
	default:
		return nil, []Refusal{{Reason: fmt.Sprintf("unknown format %q", format)}}
	}
}

// renderDotenv is internal/dotenv's standard grammar, which Parse reads back
// byte-exact. It is deliberately not Compose's `format: raw`: a generic dotenv
// consumer unquotes, so values that the bare grammar would alter are quoted.
func renderDotenv(keys []string, values map[string]string) ([]byte, []Refusal) {
	entries := make([]dotenv.Entry, 0, len(keys))
	for _, k := range keys {
		entries = append(entries, dotenv.Entry{Key: k, Value: values[k]})
	}
	out, refused, err := dotenv.Encode(entries)
	if err != nil {
		return nil, []Refusal{{Reason: err.Error()}}
	}
	if len(refused) > 0 {
		rs := make([]Refusal, 0, len(refused))
		for _, r := range refused {
			rs = append(rs, Refusal{Key: r.Key, Reason: r.Reason})
		}
		return nil, rs
	}
	return out, nil
}

// textRefusals is the shared JSON/YAML domain: a document of Unicode strings
// cannot carry invalid UTF-8 or NUL byte-exactly.
func textRefusals(keys []string, values map[string]string) []Refusal {
	var refusals []Refusal
	for _, k := range keys {
		v := values[k]
		switch {
		case !utf8.ValidString(v):
			refusals = append(refusals, Refusal{Key: k, Reason: "value is not valid UTF-8"})
		case strings.IndexByte(v, 0) >= 0:
			refusals = append(refusals, Refusal{Key: k, Reason: "NUL byte"})
		}
	}
	return refusals
}

// renderJSON is a flat object of strings: sorted keys (encoding/json sorts map
// keys), no HTML escaping, two-space indent, trailing newline.
func renderJSON(keys []string, values map[string]string) ([]byte, []Refusal) {
	if refused := textRefusals(keys, values); len(refused) > 0 {
		return nil, refused
	}
	obj := make(map[string]string, len(keys))
	for _, k := range keys {
		obj[k] = values[k]
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil {
		return nil, []Refusal{{Reason: err.Error()}}
	}
	return buf.Bytes(), nil
}

// renderYAML is a flat block mapping with every key and value double-quoted,
// so no scalar is ever resolved as a boolean, number or null (`on`, `1e3`,
// `null`, `NO` all stay strings under YAML 1.1 and 1.2 readers alike).
func renderYAML(keys []string, values map[string]string) ([]byte, []Refusal) {
	if refused := textRefusals(keys, values); len(refused) > 0 {
		return nil, refused
	}
	sorted := slices.Clone(keys)
	slices.Sort(sorted)
	var buf bytes.Buffer
	if len(sorted) == 0 {
		buf.WriteString("{}\n")
		return buf.Bytes(), nil
	}
	for _, k := range sorted {
		writeYAMLQuoted(&buf, k)
		buf.WriteString(": ")
		writeYAMLQuoted(&buf, values[k])
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// writeYAMLQuoted writes a YAML double-quoted scalar. Everything a reader
// could reject, fold, normalize or treat as a line break is escaped: C0/C1
// controls, DEL, Unicode line separators, the byte-order mark and U+FFFE/U+FFFF.
func writeYAMLQuoted(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			buf.WriteString(`\\`)
		case '"':
			buf.WriteString(`\"`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case 0x85, 0x2028, 0x2029, 0xFEFF, 0xFFFE, 0xFFFF:
			fmt.Fprintf(buf, `\u%04X`, r)
		default:
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				fmt.Fprintf(buf, `\x%02X`, r)
				continue
			}
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
}

// renderRaw is the single value's bytes, with no newline added.
func renderRaw(keys []string, values map[string]string) ([]byte, []Refusal) {
	if len(keys) != 1 {
		return nil, []Refusal{{Reason: fmt.Sprintf("raw renders exactly one key, got %d", len(keys))}}
	}
	return []byte(values[keys[0]]), nil
}
