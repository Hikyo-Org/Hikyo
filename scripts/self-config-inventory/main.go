// self-config-inventory emits the recognized configuration metadata for the
// self-configuration decision report. It does not read environment values or
// configuration files and grants no additional runtime Apply capability.
package main

import (
	"encoding/json/jsontext"
	"fmt"
	"os"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

func main() {
	if err := jsonwire.Encode(os.Stdout, config.VariableInventory(), jsontext.WithIndent("  ")); err != nil {
		fmt.Fprintln(os.Stderr, "self-configuration inventory: cannot write metadata")
		os.Exit(1)
	}
}
