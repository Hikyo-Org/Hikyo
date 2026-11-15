// self-config-inventory emits the recognized configuration metadata for the
// self-configuration decision report. It does not read environment values or
// configuration files and grants no additional runtime Apply capability.
package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

func main() {
	encoder := jsontext.NewEncoder(os.Stdout, jsonwire.Stable, jsontext.WithIndent("  "))
	if err := json.MarshalEncode(encoder, config.VariableInventory()); err != nil {
		fmt.Fprintln(os.Stderr, "self-configuration inventory: cannot write metadata")
		os.Exit(1)
	}
}
