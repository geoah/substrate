package config

import (
	"fmt"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
)

// MinChangelogSegmentBytes is the smallest segment size accepted. Below it a
// busy repository rotates every few writes, and every rotation is an fsync,
// a sidecar and a new file.
const MinChangelogSegmentBytes int64 = 1 << 20

// Data says where the server keeps every repository's directory: the
// changelog segments, the sealed store's files, the manifest and (on the fs
// blob backend) the blob bytes, one subdirectory per repository under
// <Root>/repositories. It is part of Config and also loadable on its own,
// because substratectl's operator hat opens the engine over a DSN and needs
// the same root without the rest of the server's configuration.
type Data struct {
	// Root is the data root. It must be absolute: a relative one follows the
	// process's working directory, and a store that moves when the server is
	// restarted from another directory is a store that has lost its bytes.
	Root string `envconfig:"SUBSTRATE_DATA_ROOT" required:"true"`
	// ChangelogSegmentBytes is the size past which the active changelog
	// segment rotates: the writer fsyncs, writes the finished file's sidecar
	// digest and opens the next file. 256 MiB by default.
	ChangelogSegmentBytes int64 `envconfig:"SUBSTRATE_CHANGELOG_SEGMENT_BYTES" default:"268435456"`
}

// LoadData reads the data root configuration alone, validated, for a command
// that has no use for the rest of the service configuration.
func LoadData() (Data, error) {
	var d Data
	if err := envconfig.Process("", &d); err != nil {
		return d, err
	}
	if err := d.Validate(); err != nil {
		return d, err
	}
	return d, nil
}

// Validate refuses a root that is not an absolute path and a segment size
// under MinChangelogSegmentBytes, naming the variable in either case.
func (d Data) Validate() error {
	if d.Root == "" {
		return fmt.Errorf("SUBSTRATE_DATA_ROOT is unset: it is the directory every repository's changelog, sealed store and blobs live under, and there is no default")
	}
	if !filepath.IsAbs(d.Root) {
		return fmt.Errorf("SUBSTRATE_DATA_ROOT %q must be an absolute path", d.Root)
	}
	if d.ChangelogSegmentBytes < MinChangelogSegmentBytes {
		return fmt.Errorf("SUBSTRATE_CHANGELOG_SEGMENT_BYTES is %d: it must be at least %d (1 MiB)", d.ChangelogSegmentBytes, MinChangelogSegmentBytes)
	}
	return nil
}
