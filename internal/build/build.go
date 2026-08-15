// Package build reports which build of substrate is running: the release it
// was cut from and the commit under it. Discovery serves both
// (GET /.well-known/substrate/server.json) and `substratectl version` prints
// the first, so this is the one place in the tree that answers "which
// version".
//
// A build learns its version one of two ways, and both must work:
//
//   - STAMPED. A release passes -X for the two variables below:
//     .goreleaser.yaml for the CLI and the server, the Dockerfile for the
//     image built from source. This is the only path that can name a version
//     the build context does not carry, which is why the image build has one.
//     .dockerignore drops .git, so nothing inside that context knows what tag
//     it is.
//   - RECORDED. Nothing stamped, but the toolchain saw a repository or a
//     module version: `go install ...@v0.2.0`, or a `go build` in a clean
//     checkout at a tag, both leave it in the build info. A dirty tree or a
//     commit past the tag lands there as a +dirty or a pseudo version, which
//     is honest and is meant to read as unreleased.
//
// Anything else (`go run`, `go test`) is "dev".
package build

import (
	"runtime/debug"
	"sync"
)

// version and commit are written by the linker, never assigned in Go. -X
// takes a SYMBOL PATH as a string, so renaming this package or either
// variable turns every stamp into a silent no-op that still builds and still
// reports "dev": TestStampedSymbolsAreTheOnesShipped is what refuses that.
var (
	version string
	commit  string
)

var resolved struct {
	sync.Once
	version string
	commit  string
}

// Version is the release this binary was cut from ("v0.2.0"), or "dev" when
// it was not cut from one.
func Version() string {
	resolved.Do(resolve)
	return resolved.version
}

// Commit is the full revision this binary was built from, empty when the
// build carried no way to know it.
func Commit() string {
	resolved.Do(resolve)
	return resolved.commit
}

func resolve() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}
	resolved.version, resolved.commit = choose(version, commit, info)
}

// choose folds the stamp and the build info into the pair reported. A stamp
// wins over the build info: the two agree on a release, and where they
// disagree the stamp is the deliberate one.
func choose(version, commit string, info *debug.BuildInfo) (string, string) {
	if info != nil {
		// "(devel)" is what a build with no module version records; it says
		// less than "dev" does and is not worth serving.
		if version == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		if commit == "" {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" {
					commit = s.Value
				}
			}
		}
	}
	if version == "" {
		version = "dev"
	}
	return version, commit
}
