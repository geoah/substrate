package build

import (
	"os"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

// marker exists so the test can ask the toolchain for this package's import
// path rather than spelling it, which is the half of the guard below that a
// package move must not be able to slip past.
type marker struct{}

func TestChoose(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	tagged := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: sha}},
	}
	devel := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}

	for _, tc := range []struct {
		name            string
		version, commit string
		info            *debug.BuildInfo
		wantVersion     string
		wantCommit      string
	}{
		{
			name: "a release stamp is reported as it was stamped",
			// The stamp names the tag the release was cut from even when the
			// build context carries no repository at all, which is the image
			// build's whole case.
			version: "v0.2.0", commit: sha, info: nil,
			wantVersion: "v0.2.0", wantCommit: sha,
		},
		{
			name:    "the stamp wins over what the toolchain recorded",
			version: "v0.2.0", info: tagged,
			wantVersion: "v0.2.0", wantCommit: sha,
		},
		{
			name: "an unstamped build falls back to the recorded version",
			info: tagged,
			// `go install ...@v0.1.0` and a clean checkout at the tag both
			// land here, and both are a real release: report it.
			wantVersion: "v0.1.0", wantCommit: sha,
		},
		{
			name:        "a build with no version of any kind is dev",
			info:        devel,
			wantVersion: "dev",
		},
		{
			name:        "no build info at all is dev",
			wantVersion: "dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotVersion, gotCommit := choose(tc.version, tc.commit, tc.info)
			if gotVersion != tc.wantVersion {
				t.Errorf("version = %q, want %q", gotVersion, tc.wantVersion)
			}
			if gotCommit != tc.wantCommit {
				t.Errorf("commit = %q, want %q", gotCommit, tc.wantCommit)
			}
		})
	}
}

// A test binary is stamped by nothing and records no module version, so it is
// the fallback's own case: whatever else changes, this must never report a
// version that reads like a release.
func TestUnstampedBuildIsDev(t *testing.T) {
	if got := Version(); got != "dev" {
		t.Fatalf("Version() = %q in a test binary, want %q", got, "dev")
	}
}

// The guard the package comment promises. A -X flag names a symbol as a
// STRING: move this package, rename either variable, and the linker drops the
// flag without a word, so the release builds, ships, and reports "dev". The
// variables are held by this file referencing them (a rename stops compiling)
// and the paths by comparing against the import path the toolchain reports.
func TestStampedSymbolsAreTheOnesShipped(t *testing.T) {
	_, _ = version, commit

	pkg := reflect.TypeOf(marker{}).PkgPath()
	// Only the symbol path is checked, never the whole flag: yamlfmt rewraps
	// .goreleaser.yaml's folded scalars and a Dockerfile RUN continues over
	// lines, so the surrounding text is not stable. The path carries no
	// space, so it cannot be broken across a wrap.
	want := []string{pkg + ".version=", pkg + ".commit="}

	// Every file that cuts a versioned artifact. .goreleaser.yaml stamps the
	// released CLI and server; the Dockerfile stamps the image built from
	// source, which is what `image:push` and the latest workflow ship.
	for _, name := range []string{"../../.goreleaser.yaml", "../../Dockerfile"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s stamps no %q: the build reports \"dev\" and nothing failed to say so", name, w)
			}
		}
	}
}
