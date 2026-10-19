package crypto

import (
	"bytes"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// The A6 formatting-surface coverage test: each of String / GoString /
// LogValue / MarshalText / MarshalJSON is exercised against a PLANTED
// secret, and the output must carry the marker and never the material —
// in raw, hex or JSON-escaped form. fmt's reflective struct printing is
// exactly what these surfaces exist to intercept.
func TestRedactionSurfacesAgainstPlantedSecret(t *testing.T) {
	planted := []byte("PLANTED-SECRET-0123456789abcdef01")
	kr := &Keyring{}
	kr.token.adopt(keyHandle{id: "token-root", version: 1, key: planted})
	kr.master.Store(singleMaster(1, planted))
	kr.instance.Store(&versionSet{active: 1, byVer: map[uint32]keyHandle{1: {id: "dek-instance", version: 1, key: planted}}})
	dekSet := &versionSet{active: 1, byVer: map[uint32]keyHandle{1: {id: "dek", key: planted}}}
	local := LocalKeys{stampKey: planted, snapshotKey: planted}
	sealers := map[string]any{
		"local keys value":   local,
		"local keys pointer": &local,
		"keyring":            kr,
		"project sealer":     &ProjectSealer{kr: kr, orgID: "o", projectID: "p", deks: dekSet},
		"instance sealer":    &InstanceSealer{kr: kr},
		"key handle":         keyHandle{id: "h", key: planted},
		"swap handle":        &kr.token,
		"version set":        dekSet,
		"master set":         kr.master.Load(),
		"dek entry":          dekEntry{scope: "s", set: dekSet},
	}

	hexBytes := make([]string, len(planted))
	for i, b := range planted {
		hexBytes[i] = fmt.Sprintf("%#x", b)
	}
	leaks := []string{
		string(planted),
		hex.EncodeToString(planted),
		strings.ToUpper(hex.EncodeToString(planted)),
		fmt.Sprintf("%v", planted),   // the byte-slice decimal dump reflection would print
		fmt.Sprintf("%#v", planted),  // the Go-syntax hex-byte dump
		strings.Join(hexBytes, ", "), // also catches reflection's different byte type spelling
	}
	assertClean := func(name, surface, out string) {
		t.Helper()
		if !strings.Contains(out, Redacted) {
			t.Errorf("%s: %s output %q does not carry the redaction marker", name, surface, out)
		}
		for _, leak := range leaks {
			if strings.Contains(out, leak) {
				t.Errorf("%s: %s leaks the planted secret: %q", name, surface, out)
			}
		}
	}

	for name, v := range sealers {
		assertClean(name, "%v", fmt.Sprintf("%v", v))
		assertClean(name, "%s", fmt.Sprintf("%s", v))
		assertClean(name, "%#v", fmt.Sprintf("%#v", v))
		assertClean(name, "%+v", fmt.Sprintf("%+v", v))

		lv, ok := v.(slog.LogValuer)
		if !ok {
			t.Fatalf("%s: not a slog.LogValuer", name)
		}
		assertClean(name, "LogValue", lv.LogValue().String())
		for _, jsonHandler := range []bool{false, true} {
			var out bytes.Buffer
			var handler slog.Handler = slog.NewTextHandler(&out, nil)
			if jsonHandler {
				handler = slog.NewJSONHandler(&out, nil)
			}
			slog.New(handler).Info("redaction test", "keys", v)
			assertClean(name, "slog handler", out.String())
		}

		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: json.Marshal: %v", name, err)
		}
		assertClean(name, "MarshalJSON", string(b))

		tm, ok := v.(encoding.TextMarshaler)
		if !ok {
			t.Fatalf("%s: no MarshalText", name)
		}
		tb, err := tm.MarshalText()
		if err != nil {
			t.Fatalf("%s: MarshalText: %v", name, err)
		}
		assertClean(name, "MarshalText", string(tb))
	}
}
