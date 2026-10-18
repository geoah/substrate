// synth generates synthetic provider data for one invented person, in the
// shapes the shipped provider functions read, and loads it into a running
// substrate. It is a development tool: the demo, the screenshots and the
// contract tests in internal/providertest all start from it.
//
//	synth generate [-out DIR] [-providers whoop,manual]
//	               [-from 2026-07-14] [-to 2026-10-11] [-seed ada] [-zone Europe/Athens]
//	               [-authority ada.example.com]
//	synth load -authority AUTHORITY [-dir DIR] [-server URL] [-providers ...]
//	           [-key KEY] [-replace-key] [-resume PROVIDER/NNNN] [-settle 5m]
//	           [-repo DIR] [-ctl PATH] [-python PATH]
//
// `generate` writes one directory per provider it names under DIR, replacing
// that provider's directory from an earlier run: mock recordings named the way
// internal/providere2e/runner/mockserver.py finds them (WHOOP), webhook
// requests in posting order (a provider that receives pushes) and the
// hand-entered records (`manual/labs.yaml`).
// `load` runs the mock-driven syncs through the provider e2e runner, imports
// the samples, applies the records and posts the webhook requests to the
// server's door one at a time, waiting for each run. The bearer comes from
// SUBSTRATE_TOKEN, never argv, and the webhook key from -key or
// SYNTH_WEBHOOK_KEY.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	// The zones are compiled in, so generate writes the same bytes on a host
	// whose zone database is missing or older.
	_ "time/tzdata"

	"github.com/geoah/substrate/internal/synth"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "synth:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: synth generate|load [flags]")
	}
	switch args[0] {
	case "generate":
		return generate(args[1:], stdout)
	case "load":
		return load(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q: generate or load", args[0])
	}
}

func emitters(names, authority string) ([]synth.Emitter, error) {
	all := synth.Emitters()
	for i, e := range all {
		if m, ok := e.(synth.Manual); ok {
			m.Authority = authority
			all[i] = m
		}
	}
	if names == "" {
		return all, nil
	}
	var out []synth.Emitter
	for _, n := range strings.Split(names, ",") {
		n = strings.TrimSpace(n)
		found := false
		for _, e := range all {
			if e.Name() == n {
				out = append(out, e)
				found = true
			}
		}
		if !found {
			var known []string
			for _, e := range all {
				known = append(known, e.Name())
			}
			return nil, fmt.Errorf("unknown provider %q: one of %s", n, strings.Join(known, ", "))
		}
	}
	return out, nil
}

func generate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	out := fs.String("out", ".dev/synth", "the directory to write under, one subdirectory per provider")
	providers := fs.String("providers", "", "comma-separated providers; empty is every one")
	from := fs.String("from", "", "the first day, YYYY-MM-DD (default 2026-07-14)")
	to := fs.String("to", "", "the last day, YYYY-MM-DD (default 2026-10-11)")
	seed := fs.String("seed", "", "the seed every value derives from (default ada)")
	zone := fs.String("zone", "", "the home zone (default Europe/Athens)")
	authority := fs.String("authority", "ada.example.com", "the repository the manual records are written to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts := synth.Options{Seed: *seed, HomeZone: *zone}
	var err error
	if *from != "" {
		if opts.Start, err = synth.ParseDate(*from); err != nil {
			return fmt.Errorf("-from: %w", err)
		}
	}
	if *to != "" {
		if opts.End, err = synth.ParseDate(*to); err != nil {
			return fmt.Errorf("-to: %w", err)
		}
	}
	p, err := synth.NewPersona(opts)
	if err != nil {
		return err
	}
	ems, err := emitters(*providers, *authority)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %s to %s, seed %q, home %s\n", p.Person.Name, p.Options.Start, p.Options.End, p.Options.Seed, p.Options.HomeZone)
	for _, e := range ems {
		started := time.Now()
		o, err := e.Emit(p)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		paths, err := synth.WriteOutput(*out, e, o)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		var size int64
		for _, pth := range paths {
			if st, err := os.Stat(pth); err == nil {
				size += st.Size()
			}
		}
		fmt.Fprintf(stdout, "  %-12s %4d files, %4d deliveries, %6.1f MB, %s -> %s\n",
			e.Name(), len(o.Files), len(o.Deliveries), float64(size)/1e6, time.Since(started).Round(time.Millisecond), filepath.Join(*out, e.Name()))
	}
	return nil
}
