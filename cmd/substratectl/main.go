package main

import (
	"os"

	"github.com/geoah/substrate/cmd/substratectl/commands"
)

// version is set via -ldflags at build time.
var version = "dev"

func main() {
	os.Exit(commands.Execute(version))
}
