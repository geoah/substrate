package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/vocabulary"
)

func (a *app) publishCommand() *cobra.Command {
	var check, allow bool
	var file, document, closure string
	var version int64
	cmd := &cobra.Command{
		Use:   "publish <authority/package> --check --file closure.yaml --version N --closure https://example.com/closure.yaml",
		Short: "Check an immutable package release and print its authority-document entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !check {
				return errors.New("publish requires --check; upload the checked closure and document yourself")
			}
			if file == "" || closure == "" || version < 1 {
				return errors.New("--file, --closure and a positive --version are required")
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			name, _ := vocabulary.SplitPackageRef(args[0])
			opts := authority.Options{}
			if document != "" {
				opts.LocalDocuments = map[string]string{name: document}
			}
			entry, err := checkPublication(cmd.Context(), authority.NewReader(opts), args[0], version, closure, raw, allow)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(a.out)
			enc.SetIndent("", "  ")
			return enc.Encode(entry)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "validate without publishing")
	cmd.Flags().BoolVar(&allow, "allow-narrowing", false, "allow removal or renaming from the newest release")
	cmd.Flags().StringVarP(&file, "file", "f", "", "candidate closure file")
	cmd.Flags().StringVar(&document, "document", "", "authority document file; otherwise read the live document")
	cmd.Flags().StringVar(&closure, "closure", "", "URL or development path to list for this release")
	cmd.Flags().Int64Var(&version, "version", 0, "publisher's package version")
	return cmd
}

func checkPublication(ctx context.Context, reader *authority.Reader, id string, version int64, closure string, raw []byte, allow bool) (authority.AuthorityPackageVersion, error) {
	entry := authority.AuthorityPackageVersion{}
	if len(raw) > authority.MaxClosure {
		return entry, errors.New("closure exceeds 4 MB")
	}
	candidate, err := catalog.ParseClosure(raw, id, version)
	if err != nil {
		return entry, err
	}
	source, err := reader.Read(ctx, candidate.Authority, true)
	if err != nil {
		return entry, err
	}
	sum := sha256.Sum256(raw)
	// The dialect the header states rides the entry, so a server older than
	// it passes over this version before fetching it (decision 0268).
	entry = authority.AuthorityPackageVersion{Version: int(version), Closure: closure, SHA256: hex.EncodeToString(sum[:]), Dialect: int(candidate.Dialect())}
	// Parsing the updated document holds the proposed entry to the same shape
	// as the server, without fetching a file that has not been uploaded yet.
	probe := authority.AuthorityDocument{Authority: candidate.Authority, Packages: []authority.AuthorityPackage{{Package: candidate.Package, Versions: []authority.AuthorityPackageVersion{entry}}}}
	encoded, err := json.Marshal(probe)
	if err != nil {
		return entry, err
	}
	if _, err := authority.ParseAuthorityDocument(encoded, candidate.Authority); err != nil {
		return entry, err
	}
	var newest authority.AuthorityPackageVersion
	for _, p := range source.Document.Packages {
		if p.Package != candidate.Package {
			continue
		}
		for _, v := range p.Versions {
			if int64(v.Version) == version && !strings.EqualFold(v.SHA256, entry.SHA256) {
				return entry, fmt.Errorf("version %d is already published with another digest", version)
			}
			if int64(v.Version) > version {
				return entry, fmt.Errorf("version %d is below published version %d", version, v.Version)
			}
			if v.Version > newest.Version {
				newest = v
			}
		}
	}
	if newest.Version > 0 {
		body, err := reader.Closure(ctx, source, newest)
		if err != nil {
			return entry, err
		}
		held, err := catalog.ParseClosure(body, id, int64(newest.Version))
		if err != nil {
			return entry, err
		}
		narrows, _ := catalog.CompareClosures(held, candidate)
		if narrows && !allow {
			return entry, errors.New("the candidate removes or renames declarations; review it and use --allow-narrowing")
		}
	}
	return entry, nil
}
