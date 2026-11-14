package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// pathRename is the rename door (decision record 0287), beside the delete:
// both factors in the body, no token.
const pathRename = "/repository/rename"

func (a *app) renameCommand() *cobra.Command {
	var (
		code          string
		passwordStdin bool
	)
	cmd := &cobra.Command{
		Use:   "rename <name>",
		Short: "Give your repository a new name; the old one keeps working for 90 days",
		Long: `Give the repository in the current context a new name.

The name is what you sign in with and what addresses your data. A short name
is completed under the substrate's host, as at registration. Nothing stored
changes: your own kinds are ~, and the id that keys your storage stays. The
old name keeps resolving to your repository for 90 days, on every address
(records, webhooks, apps' tokens), and nobody else can take it until then;
then it is free. The context remembers the new name.

It asks for the password of the person in the current context, and a TOTP
code where they have a second factor, because a token is no evidence for it.
Only an admin of the repository renames it.

  substratectl rename ada.example.org
  substratectl rename ada --totp-code 123456 --password-stdin < password`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			ctx, err := a.resolveContext()
			if err != nil {
				return err
			}
			identity, err := a.askIdentity(firstNonEmpty(ctx.Identity, ctx.Repository))
			if err != nil {
				return err
			}
			repository := ctx.Repository
			if repository == "" {
				if repository, err = a.prompt("Repository: "); err != nil {
					return err
				}
				if repository == "" {
					return errors.New("the repository to rename is required")
				}
			}
			password, err := a.secret(passwordStdin, "Password: ")
			if err != nil {
				return err
			}
			if code != "" {
				if code, err = normalizeCode(code); err != nil {
					return err
				}
			}
			cl := newClient(ctx.Server, "", a.hc)
			in := substrate.RenameRepositoryRequest{Identity: identity, Repository: repository, Password: password, TOTPCode: code, Name: args[0]}
			names, err := cl.renameRepository(cmd.Context(), in)
			if code == "" && isCodeRequired(err) {
				if in.TOTPCode, err = a.askCode("", "TOTP code: "); err != nil {
					return err
				}
				names, err = retryWhenPaced(cmd.Context(), a, func() (substrate.RepositoryNames, error) {
					return cl.renameRepository(cmd.Context(), in)
				})
			}
			if err != nil {
				return authError(err)
			}
			// The saved context takes the new name only where it is the
			// repository renamed: the same server, under the name the rename
			// was asked for. A --server or SUBSTRATE_SERVER override renamed
			// a repository the context does not point at.
			stored, ok := cfg.context(ctx.Name)
			switch {
			case ok && stored.Server == ctx.Server && stored.Repository == repository:
				stored.Repository = names.Name
				cfg.upsertContext(stored)
				if err := a.saveConfig(cfg); err != nil {
					return err
				}
			case ok:
				fmt.Fprintf(a.errOut, "note: the context %s points at %s on %s, so it was left as it is\n", ctx.Name, dash(stored.Repository), dash(stored.Server))
			}
			fmt.Fprintf(a.out, "renamed %s to %s on %s\n", repository, names.Name, ctx.Server)
			for _, f := range names.FormerNames {
				fmt.Fprintf(a.out, "  %s still resolves to it until %s\n", f.Name, f.Until.Local().Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&code, "totp-code", "", "current 6-digit code (prompted for when omitted and the account has a second factor)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin (one line) instead of prompting")
	return cmd
}

// renameRepository is the rename door: both factors and the new name, no
// token.
func (c *client) renameRepository(ctx context.Context, in substrate.RenameRepositoryRequest) (substrate.RepositoryNames, error) {
	var out substrate.RepositoryNames
	err := c.do(ctx, http.MethodPost, pathRename, nil, in, &out)
	return out, err
}
