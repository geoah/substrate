package engine

import (
	"archive/zip"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

var updateZones = flag.Bool("update-zones", false, "write zonenames.txt from the toolchain's zone database")

// zonenames.txt is the toolchain's zone database listed, so a Go upgrade
// that adds, removes or renames a zone fails here until it is written
// again.
func TestZoneNamesAreTheToolchainsDatabase(t *testing.T) {
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	z, err := zip.OpenReader(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "time", "zoneinfo.zip"))
	if err != nil {
		t.Fatalf("open the toolchain's zone database: %v", err)
	}
	defer func() { _ = z.Close() }()
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	want := strings.Join(names, "\n") + "\n"
	if *updateZones {
		if err := os.WriteFile("zonenames.txt", []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if zoneNamesText != want {
		t.Fatalf("zonenames.txt is not the toolchain's zone database (%d names there, %d in the file): go test ./internal/engine/ -run TestZoneNamesAreTheToolchainsDatabase -update-zones",
			len(names), len(zoneNames()))
	}
	if !slices.IsSorted(zoneNames()) {
		t.Fatal("zonenames.txt is not in byte order, which the lookup needs")
	}
}

// A written zone is a name the IANA database holds, spelled as it spells
// it, whatever this host's own zone files answer to: another case, a space,
// or a file of the host's tzdata that is not a zone name is refused; an
// alias the database keeps is admitted (finding 2 of step 33's second
// review).
func TestAZoneIsSpelledAsTheIANADatabaseSpellsIt(t *testing.T) {
	t.Parallel()
	p := &vocabulary.Property{Name: "zone", Datatype: vocabulary.DatatypeTimezone}
	for _, zone := range []string{"Europe/athens", "europe/Athens", "EUROPE/ATHENS", " Europe/Athens", "Europe/Athens ", "UTC ", "posixrules", "zone.tab"} {
		_, err := coerceScalar(p, zone)
		if err == nil || !strings.Contains(err.Error(), "spelled as the IANA database spells it") {
			t.Errorf("zone %q: %v, want it refused as not the database's spelling", zone, err)
		}
	}
	for _, zone := range []string{"UTC", "Europe/Athens", "America/New_York", "US/Pacific", "Asia/Calcutta", "Etc/GMT+0"} {
		if _, err := coerceScalar(p, zone); err != nil {
			t.Errorf("zone %q refused: %v", zone, err)
		}
	}
}
