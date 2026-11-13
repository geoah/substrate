package config

import (
	"fmt"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
)

// Data says where the server keeps what Postgres does not hold of a
// repository: its blob bytes and its webhook spool, one subdirectory per
// repository under <Root>/repositories, and, by default, the archive
// (decision 0262). It is part of Config and also loadable on its own,
// because substratectl's operator hat opens the engine over a DSN and needs
// the same root without the rest of the server's configuration.
type Data struct {
	// Root is the data root. It must be absolute: a relative one follows the
	// process's working directory, and a store that moves when the server is
	// restarted from another directory is a store that has lost its bytes.
	Root string `envconfig:"SUBSTRATE_DATA_ROOT" required:"true"`
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

// Validate refuses a root that is not an absolute path, naming the variable.
func (d Data) Validate() error {
	if d.Root == "" {
		return fmt.Errorf("SUBSTRATE_DATA_ROOT is unset: it is the directory every repository's blobs, webhook spool and, by default, archive live under, and there is no default")
	}
	if !filepath.IsAbs(d.Root) {
		return fmt.Errorf("SUBSTRATE_DATA_ROOT %q must be an absolute path", d.Root)
	}
	return nil
}
