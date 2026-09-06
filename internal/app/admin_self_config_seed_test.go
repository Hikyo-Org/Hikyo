package app

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

func TestAdminCreateAdoptsRunningServerSeedInsteadOfCommandDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "admin-seed.db")
	listen := availableNodeAddress(t)
	serverCfg, _, err := config.LoadBootstrap("server", []string{"--dev", "--listen", listen}, func(key string) string {
		switch key {
		case "HIKYO_DB":
			return "sqlite:" + dbPath
		case "HIKYO_OPERATIONAL_LISTEN":
			return "127.0.0.1:0"
		case "HIKYO_EXTERNAL_ORIGIN":
			return "https://running-server.example"
		case "HIKYO_ARGON2_TIME":
			return "4"
		case "HIKYO_UPDATE_CHANNEL":
			return "nightly"
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Enrolled standalone deployments pin an explicit server identity.
	serverCfg.NodeID = "enrolled-server"
	srv, err := Boot(t.Context(), serverCfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	adminCfg, _, err := config.Load("admin", []string{"--dev"}, func(key string) string {
		if key == "HIKYO_DB" {
			return "sqlite:" + dbPath
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if adminCfg.Listen == listen {
		t.Fatal("fixture command listener does not differ")
	}
	if err := RunAdmin(t.Context(), adminCfg, testLogger(), []string{"create", "--username", "owner", "--output-file", filepath.Join(t.TempDir(), "authority.txt")}, io.Discard, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.selfConfig.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	bundle, err := srv.selfConfig.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	owner := bundle.OwnerValues()
	if owner["HIKYO_ARGON2_TIME"] != "4" || owner["HIKYO_EXTERNAL_ORIGIN"] != "https://running-server.example" || bundle.UpdateChannel() != "nightly" {
		t.Fatal("CLI defaults replaced running server owner settings")
	}
	node, err := bundle.NodeValues(srv.selfConfig.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node["HIKYO_LISTEN"] != listen || node["HIKYO_OPERATIONAL_LISTEN"] != "127.0.0.1:0" {
		t.Fatal("CLI defaults replaced exact server listeners")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + listen + "/api/v1/meta")
	if err != nil {
		t.Fatalf("original listener lost after admin provisioning: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("original listener returned %d", response.StatusCode)
	}
}

func TestAdminCreateRequiresFreshServerSeed(t *testing.T) {
	cfg := devConfig(t)
	auth, closeDB, err := adminAuth(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if _, err := auth.BootstrapAdmin(t.Context(), "owner", "Owner", "stdout"); err == nil {
		t.Fatal("host command invented server settings without running server seed")
	}
	// A refusal occurs before creating a principal: starting the server permits
	// the same first-admin operation to succeed with authenticated server input.
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if _, err := auth.BootstrapAdmin(t.Context(), "owner", "Owner", "stdout"); err != nil {
		t.Fatalf("missing seed refusal consumed initial setup: %v", err)
	}
}
