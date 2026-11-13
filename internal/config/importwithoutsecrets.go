package config

import (
	"strings"

	"github.com/kelseyhightower/envconfig"
)

// SplitRepositoryList is how the server and the operator's commands read
// SUBSTRATE_IMPORT_WITHOUT_SECRETS (decision 0264), the repositories a
// restore may take without the secrets this host's key does not open, alike: one repository between commas,
// spaces around it dropped, an empty entry ignored.
func SplitRepositoryList(list string) []string {
	var out []string
	for _, r := range strings.Split(list, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// LoadImportWithoutSecrets reads SUBSTRATE_IMPORT_WITHOUT_SECRETS from the
// environment for a process that is not the server, which reads it as part
// of Config: an operator command that opens as the writer restores and takes
// archives as the server's start does, under the same listing.
func LoadImportWithoutSecrets() ([]string, error) {
	var c struct {
		List string `envconfig:"SUBSTRATE_IMPORT_WITHOUT_SECRETS" default:""`
	}
	if err := envconfig.Process("", &c); err != nil {
		return nil, err
	}
	return SplitRepositoryList(c.List), nil
}
