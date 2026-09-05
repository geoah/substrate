package commands

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// catalogPath is /api/v1/catalog/{id}/{verb}: the shipped closures, at the
// version root because the endpoint names no kind (decision 0033). The bundle
// id is a package reference and carries a `/`, so it is escaped as one
// segment.
func catalogPath(id, verb string) string {
	return apiPrefix + "/catalog/" + url.PathEscape(id) + "/" + verb
}

// takeBundle runs one of the two catalog doors and prints what landed. The
// server answers with the LANDED bundle's status, which for an import is the
// sample's package under this repository's own authority rather than the id
// typed.
func (a *app) takeBundle(cmd *cobra.Command, id, verb, past string) error {
	cl, err := a.client()
	if err != nil {
		return err
	}
	var st substrate.BundleStatus
	if err := cl.do(cmd.Context(), http.MethodPost, catalogPath(id, verb), nil, nil, &st); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s %s\n", st.ID, past)
	printBundleStatus(a, st)
	return nil
}

// importCommand is the SAMPLE door (decision record 0048). A sample is
// vocabulary to copy: the server rehomes the closure onto this repository's
// own authority and admits it there, so `samples.substrate.reamde.dev/tasks`
// lands as `<your authority>/tasks` and is yours to edit afterwards.
func (a *app) importCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "import <sample>",
		Short: "Copy a shipped sample under your own authority",
		Long: `Import one of the shipped SAMPLE packages into this repository.

A sample is vocabulary to copy, not vocabulary to depend on. The server
rewrites the closure's placeholder authority to the one this repository owns
before admitting it, so

  substratectl import samples.substrate.reamde.dev/tasks

lands ` + "`<your authority>/tasks/task`" + `: your kind, writable through the
API and never offered an upgrade. A sample that declares against another is
refused until that one is imported, naming what to import first.

Providers take the other door, ` + "`substratectl install`" + `, and land under
the authority that publishes them. Importing a provider is refused, naming it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.takeBundle(cmd, args[0], "import", "imported")
		},
	}
}

// installCommand is the PROVIDER door: the closure lands verbatim, under the
// authority that publishes it, and the publisher's next version bump is what
// the console offers as an upgrade.
func (a *app) installCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install <provider>",
		Short: "Install a shipped provider under the authority that publishes it",
		Long: `Install one of the shipped PROVIDER packages into this repository.

A provider is a package its publisher owns, so it lands exactly as published:

  substratectl install providers.substrate.reamde.dev/google

Its declarations are the publisher's afterwards (` + "`source: published`" + `),
so this repository's token may not rewrite them, and each change the publisher
ships arrives as an upgrade. Re-running this command is that upgrade.

Samples take the other door, ` + "`substratectl import`" + `, and land under
your own authority.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.takeBundle(cmd, args[0], "install", "installed")
		},
	}
}
