package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// publishRelease is `publish <authority/package> --release`: the next release
// of a package this repository publishes, the package's declarations as they
// are now at its next version. `release` alone is the marks' word (decision
// 0284), so a package's release is a form of publish.
func (a *app) publishRelease(ctx context.Context, arg, output string) error {
	if err := outputFormat(output); err != nil {
		return err
	}
	pkg := unexpandedLocal(arg)
	owner, _ := vocabulary.SplitPackageRef(pkg)
	switch owner {
	case "":
		return fmt.Errorf("%q names no package: --release takes <authority>/<package>, one this repository publishes", arg)
	case vocabulary.Local:
		return fmt.Errorf("%s is not published yet: publish it first with substratectl publish '%s' --as <authority>", pkg, pkg)
	}
	cl, err := a.client()
	if err != nil {
		return err
	}
	var res substrate.Release
	if err := cl.do(ctx, http.MethodPost, pathRelease, nil, substrate.ReleaseInput{Package: pkg}, &res); err != nil {
		return refused(err, "the release was refused", releaseHint(err))
	}
	return printAs(a, output, res, func() error {
		fmt.Fprintf(a.out, "wrote release %d of %s\n\n", res.Version, res.Package)
		return printRelease(a.out, res)
	})
}

// releaseHint is the next step after a refused release, by what refused it.
func releaseHint(err error) string {
	var ae *apiError
	if !errors.As(err, &ae) {
		return ""
	}
	switch ae.Status {
	case http.StatusConflict:
		return "no release was written: change the package's declarations (substratectl apply), then write the next release again"
	case http.StatusForbidden:
		return "no release was written: the authority's _substrate record must name this repository's document, as substratectl publish printed it"
	case http.StatusNotFound:
		return "no release was written: --release takes a package this repository publishes"
	}
	return "no release was written"
}

// refused words a refusal of a publish or a release: the server's message
// says why, and a record write's headline and hint ("the record changed
// since it was read") would send the reader after the wrong thing. A refusal
// of the token or of the pace keeps its own.
func refused(err error, headline, hint string) error {
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status < 400 || ae.Status >= 500 ||
		ae.Status == http.StatusUnauthorized || ae.Status == http.StatusTooManyRequests {
		return err
	}
	ae.Headline = headline
	if hint != "" {
		ae.Hint = hint
	}
	return ae
}

func printRelease(w io.Writer, r substrate.Release) error {
	tw := newTable(w)
	fmt.Fprintf(tw, "  package:\t%s\n", r.Package)
	fmt.Fprintf(tw, "  version:\t%d\n", r.Version)
	fmt.Fprintf(tw, "  sha256:\t%s\n", r.SHA256)
	if r.Dialect > 0 {
		fmt.Fprintf(tw, "  dialect:\t%d\n", r.Dialect)
	}
	fmt.Fprintf(tw, "  released:\t%s\n", r.ReleasedAt.Format(time.RFC3339))
	return tw.Flush()
}
