package app

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

func TestFreshBootstrapOriginAuthoritySurvivesBoot(t *testing.T) {
	for _, kind := range []string{"explicit IP", "explicit hostname", "derived"} {
		t.Run(kind, func(t *testing.T) {
			listen := availableNodeAddress(t)
			_, port, err := net.SplitHostPort(listen)
			if err != nil {
				t.Fatal(err)
			}
			origin := ""
			switch kind {
			case "explicit IP":
				origin = "http://" + listen
			case "explicit hostname":
				origin = "http://" + net.JoinHostPort("localhost", port)
			}
			dbPath := filepath.Join(t.TempDir(), "bootstrap-origin.db")
			cfg, _, err := config.LoadBootstrap("server", []string{"--dev", "--listen", listen}, func(key string) string {
				switch key {
				case "HIKYO_DB":
					return "sqlite:" + dbPath
				case "HIKYO_OPERATIONAL_LISTEN":
					return "127.0.0.1:0"
				case "HIKYO_EXTERNAL_ORIGIN":
					return origin
				}
				return ""
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			srv, err := Boot(t.Context(), cfg, testLogger())
			if kind == "explicit IP" {
				if srv != nil {
					srv.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "hostname") {
					t.Fatalf("explicit IP bootstrap must refuse before serving: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer srv.Close()
			effective := srv.owner.current.graph.cfg
			if effective.ExternalOrigin != "http://"+net.JoinHostPort("localhost", port) || effective.ExternalOriginExplicit != (kind == "explicit hostname") {
				t.Fatalf("boot origin=%q explicit=%t", effective.ExternalOrigin, effective.ExternalOriginExplicit)
			}
		})
	}
}
