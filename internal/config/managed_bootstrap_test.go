package config

import "testing"

func TestManagedBootstrapSourcesAreAliasesOnly(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"database_source":"postgres-next","root_source":"root-next"}`,
		`{"version":1,"database_source":"postgres-current"}`,
	} {
		if _, err := ParseManagedBootstrapSources(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		`{"version":1,"database_source":"postgres://user:password@host/db"}`,
		`{"version":1,"root_source":"/run/secrets/root"}`,
		`{"version":1,"root_source":"../root"}`,
		`{"version":1,"root_source":"a","root_source":"b"}`,
		`{"Version":1,"root_source":"a"}`,
		`{"version":1,"root_source":null}`,
		`{"version":2,"root_source":"a"}`,
		`{"version":1,"root_source":"a","root_key":"secret"}`,
		`{"version":1}`,
	} {
		if _, err := ParseManagedBootstrapSources(raw); err == nil {
			t.Fatal("unsafe bootstrap source document accepted")
		}
	}
}
