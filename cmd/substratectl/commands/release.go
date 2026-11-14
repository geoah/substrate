package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// publishRelease is `publish <authority/package> --release`: the next release
// of a package this repository publishes, the package's declarations as they
// are now at its next version. `release` alone is the marks' word (decision
// 0284), so a package's release is a form of publish. It shows what the
// release will hold, as the server answers it, and writes it once asked
// (yes for a script, which writes without asking).
func (a *app) publishRelease(ctx context.Context, arg string, yes bool, output string) error {
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
	if !yes {
		var plan substrate.ReleasePlan
		if err := cl.do(ctx, http.MethodPost, pathRelease, nil, substrate.ReleaseInput{Package: pkg}, &plan); err != nil {
			return refused(err, "the release was refused", releaseHint(err))
		}
		if output != "" {
			// A plan asked for as data is answered as data, and writes
			// nothing: a script that wants the release passes --yes.
			return printAs(a, output, plan, nil)
		}
		printReleasePlan(a.out, plan)
		if len(plan.Blockers) > 0 {
			return fmt.Errorf("release %d of %s is refused now; nothing was written", plan.Version, plan.Package)
		}
		answer, err := a.prompt(fmt.Sprintf("Write release %d of %s? It is served for good and never changed. [y/N] ", plan.Version, plan.Package))
		if err != nil || !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Fprintf(a.out, "\nNothing was written. Write it with: substratectl publish %s --release --yes\n", pkg)
			return nil
		}
	}
	var res substrate.ReleasePlan
	if err := cl.do(ctx, http.MethodPost, pathRelease, nil, substrate.ReleaseInput{Package: pkg, Confirm: true}, &res); err != nil {
		return refused(err, "the release was refused", releaseHint(err))
	}
	if res.Release == nil {
		return fmt.Errorf("the server answered no release for %s", pkg)
	}
	return printAs(a, output, *res.Release, func() error {
		fmt.Fprintf(a.out, "wrote release %d of %s\n\n", res.Release.Version, res.Release.Package)
		return printRelease(a.out, *res.Release)
	})
}

// printReleasePlan is what the next release will hold, as the server
// answers it before it is written.
func printReleasePlan(w io.Writer, p substrate.ReleasePlan) {
	fmt.Fprintf(w, "release %d of %s (the last was %d)\n", p.Version, p.Package, p.Previous)
	if p.SHA256 != "" {
		fmt.Fprintf(w, "  sha256: %s\n", p.SHA256)
	}
	fmt.Fprintf(w, "  holds %d declarations:\n", len(p.Declarations))
	for _, d := range p.Declarations {
		fmt.Fprintf(w, "    %s\n", d)
	}
	if len(p.Blockers) > 0 {
		fmt.Fprintln(w, "  refused now, because:")
		for _, b := range p.Blockers {
			fmt.Fprintf(w, "    %s\n", b)
		}
	}
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
