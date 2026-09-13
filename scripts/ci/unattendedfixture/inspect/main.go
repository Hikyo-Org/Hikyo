// inspect reads the durable SQLite admission state for disposable acceptance.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: inspect SQLITE_DATABASE")
		os.Exit(2)
	}
	state, err := upgrade.InspectControl(context.Background(), upgrade.Config{Engine: releaseidentity.SQLite, Path: os.Args[1]})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Instance    string `json:"instance"`
		Version     string `json:"version"`
		Maintenance bool   `json:"maintenance"`
		Phase       string `json:"phase"`
	}{state.InstanceID, state.Applied.Release.Version, state.Maintenance, string(state.Pending.Phase)}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
