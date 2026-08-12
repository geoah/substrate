package commands

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// A token is a RECORD in the repository and has FULL ACCESS to
// it: no scopes, no actor set, no roles. So the whole of a token is a label,
// an optional expiry, and the secret shown once at mint — and these three
// commands are the whole of managing one.

func (a *app) tokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "token",
		Short:   "Mint, list and revoke tokens",
		Aliases: []string{"tokens"},
	}
	cmd.AddCommand(a.tokenCreateCommand(), a.tokenListCommand(), a.tokenRevokeCommand())
	return cmd
}

func (a *app) tokenCreateCommand() *cobra.Command {
	var (
		label   string
		expires string
	)
	cmd := &cobra.Command{
		Use:   "create --label LABEL",
		Short: "Mint a token for a script or a device",
		Long: `Mint a token with the token already in the context.

The secret is printed exactly once — the substrate stores only its SHA-256 and
cannot show it again. A token has full access to the repository; the only thing
that limits one is deleting it.

--expires takes a duration (720h) or an RFC3339 instant; without it the token
lives until it is revoked.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if label == "" {
				return errors.New("a token label is required: pass --label")
			}
			expiresAt, err := parseExpiry(expires, a.now())
			if err != nil {
				return err
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			res, err := cl.mintToken(cmd.Context(), label, expiresAt)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "token %s created\n", dash(firstNonEmpty(res.Token.Label, label)))
			if res.Token.ID != "" {
				fmt.Fprintf(a.out, "  id:      %s\n", res.Token.ID)
			}
			if res.Token.ExpiresAt != nil {
				fmt.Fprintf(a.out, "  expires: %s\n", res.Token.ExpiresAt.UTC().Format(time.RFC3339))
			}
			if res.Secret == "" {
				fmt.Fprintln(a.errOut, "warning: the response carried no secret")
				return nil
			}
			fmt.Fprintf(a.out, "\n  secret (shown once, stored hashed): %s\n", res.Secret)
			fmt.Fprintln(a.out, "  store it now; minting another is the only way to get one again")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&label, "label", "", "what this token is for")
	f.StringVar(&expires, "expires", "", "expiry as a duration (720h) or an RFC3339 instant")
	return cmd
}

func (a *app) tokenListCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List this repository's tokens (never a secret)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			tokens, err := cl.tokens(cmd.Context())
			if err != nil {
				return err
			}
			switch output {
			case "", "table":
				return printTokenTable(a.out, tokens, a.now())
			case "json":
				return printJSON(a.out, tokens)
			}
			return fmt.Errorf("unknown output format %q: use table or json", output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json")
	return cmd
}

func (a *app) tokenRevokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "revoke <id>",
		Short:   "Revoke a token by deleting its record",
		Aliases: []string{"delete"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			if err := cl.revokeToken(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "token %s revoked\n", args[0])
			return nil
		},
	}
}

// parseExpiry reads --expires as either a duration from now or an instant.
// Both forms are useful and neither is guessable from the other, so the CLI
// takes both rather than making the caller do date arithmetic.
func parseExpiry(in string, now time.Time) (*time.Time, error) {
	if in == "" {
		return nil, nil
	}
	if d, err := time.ParseDuration(in); err == nil {
		if d <= 0 {
			return nil, fmt.Errorf("--expires %q is not in the future", in)
		}
		t := now.Add(d).UTC()
		return &t, nil
	}
	t, err := time.Parse(time.RFC3339, in)
	if err != nil {
		return nil, fmt.Errorf("--expires %q is neither a duration (720h) nor an RFC3339 instant", in)
	}
	t = t.UTC()
	return &t, nil
}

func printTokenTable(w io.Writer, tokens []substrate.TokenInfo, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "ID\tLABEL\tCREATED\tEXPIRES\tLAST USED")
	for _, t := range tokens {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			dash(t.ID), dash(t.Label), humanAge(now, t.Created),
			instant(t.ExpiresAt), age(now, t.LastUsedAt))
	}
	return tw.Flush()
}

// instant renders an optional timestamp; a token without an expiry lives until
// it is deleted, and "never" says so.
func instant(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.UTC().Format(time.RFC3339)
}

func age(now time.Time, t *time.Time) string {
	if t == nil {
		return "-"
	}
	return humanAge(now, *t)
}
