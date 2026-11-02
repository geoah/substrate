package commands

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// The domain verification routes (decision 0208), owner-only, at the version
// root beside /export.
const (
	domainPath       = "/api/v1/repository/domain"
	domainVerifyPath = "/api/v1/repository/domain/verify"
)

// errDomainUnverified is the exit a failed `domain verify` ends with, after
// printing the reason, so a script can wait on the record propagating.
var errDomainUnverified = errors.New("the repository's name is not verified")

func (a *app) domainCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Prove the repository's name with a TXT record in its DNS",
		Long: `Registration takes a repository name on its owner's word. Verify domain
proves it afterwards: show prints a TXT record to add at
_substrate.<repository>, carrying this server's origin and a one-time proof,
and verify reads the record back. Once a check passes, the repository record
carries domainVerifiedAt, the proof is spent, and the record may keep only
its server= field.

A repository named under the server's own host (ada.substrate.example.com on
substrate.example.com) is verified without a record. A server reached on a
loopback or private address cannot verify any other name, because the check
fetches the record's server= origin and never dials such an address.`,
	}
	cmd.AddCommand(a.domainShowCommand(), a.domainVerifyCommand())
	return cmd
}

func (a *app) domainShowCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the TXT record that proves the repository's name, minting its proof when none is pending",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := outputFormat(output); err != nil {
				return err
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res substrate.DomainProof
			if err := cl.do(cmd.Context(), http.MethodPost, domainPath, nil, nil, &res); err != nil {
				return err
			}
			return printAs(a, output, res, func() error {
				tw := newTable(a.out)
				fmt.Fprintln(tw, "NAME\tTYPE\tVALUE")
				fmt.Fprintf(tw, "%s\tTXT\t%q\n", res.Name, res.Record)
				if err := tw.Flush(); err != nil {
					return err
				}
				if res.VerifiedAt != nil {
					fmt.Fprintf(a.out, "\nverified %s; publishing this record and checking again renews it\n", res.VerifiedAt.Format(time.RFC3339))
				} else {
					fmt.Fprintln(a.out, "\nadd the record to the name's DNS, then run: substratectl domain verify")
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json|yaml")
	return cmd
}

func (a *app) domainVerifyCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Check the repository's _substrate record and stamp the repository verified when it passes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := outputFormat(output); err != nil {
				return err
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res substrate.DomainCheck
			if err := cl.do(cmd.Context(), http.MethodPost, domainVerifyPath, nil, nil, &res); err != nil {
				return err
			}
			if err := printAs(a, output, res, func() error {
				if res.Verified {
					fmt.Fprintf(a.out, "verified %s\n", res.VerifiedAt.Format(time.RFC3339))
					return nil
				}
				fmt.Fprintf(a.out, "not verified: %s\n", res.Reason)
				if res.VerifiedAt != nil {
					fmt.Fprintf(a.out, "the earlier verification of %s stands\n", res.VerifiedAt.Format(time.RFC3339))
				}
				return nil
			}); err != nil {
				return err
			}
			if !res.Verified {
				return errDomainUnverified
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json|yaml")
	return cmd
}
