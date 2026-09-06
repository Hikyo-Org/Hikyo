// self-config-inventory emits the recognized configuration metadata for the
// self-configuration decision report. It does not read environment values or
// configuration files and grants no additional runtime Apply capability.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config.VariableInventory()); err != nil {
		fmt.Fprintln(os.Stderr, "self-configuration inventory: cannot write metadata")
		os.Exit(1)
	}
}
