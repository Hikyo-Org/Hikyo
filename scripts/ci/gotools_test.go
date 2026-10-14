package ci

import "testing"

func TestGoToolVersionFailsClosed(t *testing.T) {
	const module = "golang.org/x/vuln"
	for name, manifest := range map[string]string{
		"missing":         "golang.org/x/tools@v0.50.0\n",
		"missing-at":      module + "\n",
		"empty":           module + "@\n",
		"duplicate":       module + "@v1.7.0\n" + module + "@v1.7.0\n",
		"conflicting":     module + "@v1.7.0\n" + module + "@v1.8.0\n",
		"unpinned":        module + "@latest\n",
		"extra-at":        module + "@v1.7.0@v1.8.0\n",
		"missing-prefix":  module + "@1.7.0\n",
		"partial-version": module + "@v1.7\n",
		"leading-zero":    module + "@v01.7.0\n",
		"whitespace":      module + "@v1.7.0 \n",
	} {
		t.Run(name, func(t *testing.T) {
			if version, err := goToolVersion(manifest, module); err == nil || version != "" {
				t.Fatalf("invalid pin accepted: %q, %v", version, err)
			}
		})
	}
}

func TestGoToolVersionTracksManifestUpgrade(t *testing.T) {
	const module = "golang.org/x/vuln"
	for _, pin := range []string{"v1.7.0", "v1.8.0"} {
		version, err := goToolVersion("golang.org/x/tools@v0.50.0\n"+module+"@"+pin+"\n", module)
		if err != nil || version != pin {
			t.Fatalf("manifest pin %s: got %q, %v", pin, version, err)
		}
	}
}

func TestEmbeddedGoToolVersion(t *testing.T) {
	if version, err := GoToolVersion("golang.org/x/vuln"); err != nil || version == "" {
		t.Fatalf("embedded scanner pin: %q, %v", version, err)
	}
}
