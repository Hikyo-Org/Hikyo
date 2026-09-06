package config

import "testing"

func TestManagedNextRootSelectorIsAliasOnlyAndClearsStartupPath(t *testing.T) {
	for _, alias := range []string{"next-root", "/run/key", "../key", "{\"key\":\"secret\"}", "", "not an alias"} {
		t.Run(alias, func(t *testing.T) {
			values := managedNodeTestValues()
			values[ManagedNewRootSourceKey] = alias
			got, err := parseManagedNodeValues(&Config{NewRootKeyFile: "/stale/startup/key"}, values)
			if alias != "next-root" {
				if err == nil {
					t.Fatal("invalid root selector accepted")
				}
				return
			}
			if err != nil || got.NewRootSource != alias || got.NewRootKeyFile != "" {
				t.Fatalf("projection: %+v %v", got, err)
			}
		})
	}
	values := managedNodeTestValues()
	got, err := parseManagedNodeValues(&Config{NewRootKeyFile: "/stale/startup/key", NewRootSource: "stale-alias"}, values)
	if err != nil || got.NewRootSource != "" || got.NewRootKeyFile != "" {
		t.Fatalf("clearing revived startup source: %+v %v", got, err)
	}
}
