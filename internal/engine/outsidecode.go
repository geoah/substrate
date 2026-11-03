package engine

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/sandbox"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// WithOutsideCode controls code obtained from authority documents. The insecure
// exception lifts only the confinement requirement, never an explicit refusal.
func WithOutsideCode(policy string, unconfined bool) Option {
	return func(o *options) { o.outsideCode = policy; o.unconfinedOutsideCode = unconfined }
}

// admitOutsidePolicy refuses outside code of any form (a function, an agent,
// a trigger, a mapping or an oauth2 block) where the operator refuses it.
func (s *service) admitOutsidePolicy() error {
	if s.outsideCode == "refuse" {
		return fmt.Errorf("%w: this server refuses code from outside authorities (SUBSTRATE_OUTSIDE_CODE=refuse)", substrate.ErrGuard)
	}
	return nil
}

// admitOutsideBodies is admitOutsidePolicy for function bodies, which run in
// the sandbox and so need one that confines them: filesystem, syscalls and
// the connect gate. An agent, a trigger, a mapping and an oauth2 block run in
// the server's own process under the engine's rules (an agent's reach and
// cap, decisions 0215 and 0217), so a host the sandbox cannot fully confine,
// such as a kernel without Landlock, still takes them.
func (s *service) admitOutsideBodies() error {
	if err := s.admitOutsidePolicy(); err != nil {
		return err
	}
	c := runner.Shared.Sandbox()
	r := c.Report()
	if !s.unconfinedOutsideCode && (c.Mode() == sandbox.ModeOff || !r.Supported() || !r.FS() || !r.Seccomp || !r.ConnectGate) {
		return fmt.Errorf("%w: outside code needs a sandbox that confines it; this host cannot provide one (SUBSTRATE_INSECURE_UNCONFINED_OUTSIDE_CODE permits unconfined execution)", substrate.ErrGuard)
	}
	return nil
}

func (ds *dataset) outsidePackage(ctx context.Context, pkg string) (bool, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: kindPackage, ID: pkg})
	if err != nil {
		return false, err
	}
	if row == nil || row.DeletedAt != nil {
		return false, nil
	}
	from, _ := row.Props["publishedFrom"].(string)
	return from != "", nil
}

func (ds *dataset) admitOutsidePackage(ctx context.Context, pkg string) error {
	outside, err := ds.outsidePackage(ctx, pkg)
	if err != nil {
		return err
	}
	if outside {
		return ds.svc.admitOutsidePolicy()
	}
	return nil
}

func (ds *dataset) admitOutsideClosure(ctx context.Context, docs []vocabulary.Document, data []substrate.PutInput, opts substrate.BundleInstall) error {
	outside := opts.PublishedFrom != ""
	for _, d := range docs {
		if d.Kind != vocabulary.DocBundle {
			continue
		}
		held, err := ds.outsidePackage(ctx, d.DeclaredPackage())
		if err != nil {
			return err
		}
		outside = outside || held
	}
	if !outside {
		return nil
	}
	code, bodies, err := validateOutsideCode(docs, data)
	switch {
	case err != nil:
		return err
	case bodies:
		return ds.svc.admitOutsideBodies()
	case code:
		return ds.svc.admitOutsidePolicy()
	}
	return nil
}

// validateOutsideCode reports whether an outside closure carries code, and
// whether any of it is a function body the sandbox runs, and refuses a
// function network host or an oauth2 endpoint an outside package may not
// name.
func validateOutsideCode(docs []vocabulary.Document, data []substrate.PutInput) (code, bodies bool, err error) {
	for _, d := range docs {
		switch d.Kind {
		case vocabulary.DocFunction, vocabulary.DocAgent, vocabulary.DocRecordMapping:
			code = true
			if d.Kind == vocabulary.DocFunction {
				bodies = true
				permissions, _ := d.Data["permissions"].(map[string]any)
				items := asItems(permissions["network"])
				if hosts, ok := permissions["network"].([]string); ok {
					items = nil
					for _, host := range hosts {
						items = append(items, host)
					}
				}
				for _, raw := range items {
					host, _ := raw.(string)
					plain := strings.TrimPrefix(host, "*.")
					if net.ParseIP(plain) != nil || strings.ContainsAny(plain, ":/") || plain == "" {
						return false, false, fmt.Errorf("%w: outside function %s network must name exact DNS hosts or *.example.com patterns", substrate.ErrValidation, d.ID)
					}
				}
			}
		case vocabulary.DocBundle:
			if raw, ok := d.Data["oauth2"]; ok {
				code = true
				m, _ := raw.(map[string]any)
				for _, key := range []string{"authorizationEndpoint", "tokenEndpoint", "revocationEndpoint", "emailEndpoint"} {
					value, _ := m[key].(string)
					if value == "" {
						continue
					}
					u, err := url.Parse(value)
					if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
						return false, false, fmt.Errorf("%w: outside package %s oauth2.%s must be an HTTPS endpoint", substrate.ErrValidation, d.ID, key)
					}
				}
			}
		}
	}
	for _, d := range data {
		code = code || d.Kind == typeTrigger
	}
	return code, bodies, nil
}

// outsideInstallKey marks bodies before their package row exists. It is never
// derived from a manifest field: only the catalog's provenance sets it.
type outsideInstallKey struct{}

func (ds *dataset) outsideSpec(ctx context.Context, spec runner.Spec, pkg string) runner.Spec {
	outside, err := ds.outsidePackage(ctx, pkg)
	if installing, _ := ctx.Value(outsideInstallKey{}).(bool); installing {
		outside = true
	}
	spec.Outside = outside
	spec.UnconfinedOutside = ds.svc.unconfinedOutsideCode
	spec.Refusal = err
	if err == nil && outside {
		spec.Refusal = ds.svc.admitOutsideBodies()
	}
	return spec
}

func (t *txn) admitOutsideMapping(pkg string) error {
	row, err := t.loadRow(eref{Kind: kindPackage, ID: pkg}, false)
	if err != nil {
		return err
	}
	if row != nil {
		if from, _ := row.Props["publishedFrom"].(string); from != "" {
			return t.ds.svc.admitOutsidePolicy()
		}
	}
	return nil
}
