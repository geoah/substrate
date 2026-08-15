package main

import (
	"os"

	"github.com/geoah/substrate/cmd/substratectl/commands"
	"github.com/geoah/substrate/internal/build"
)

func main() {
	os.Exit(commands.Execute(build.Version()))
}
