package main

import (
	"log/slog"
	"os"

	"github.com/geoah/substrate/cmd/substratectl/commands"
	"github.com/geoah/substrate/internal/build"
)

func main() {
	// slog's built-in default prints a message unquoted through the stdlib
	// log package. stdout is a command's data, so lines go to stderr, and
	// only warnings and worse: the operator hat builds its own logger.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	os.Exit(commands.Execute(build.Version()))
}
