package engine

// The isolation fails CLOSED: if the two bound roles are
// absent or misconfigured, Open refuses rather than running the scoped pools as
// the DSN's own user — which, under the production superuser DSN, would bypass
// FORCE ROW LEVEL SECURITY and let every repository read every other. These are
// INTERNAL tests: they drive requireRoles/assertPoolPrincipal and Open itself.

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/geoah/substrate/internal/testdb"
)

// TestValidateRoleAttrs pins the fail-closed predicate: the safe pair is
// accepted, and every unsafe attribute — a missing role, a bypassing or
// superuser app role, a maint role that is a superuser or lacks bypass — is
// refused. This is where checking rolname alone (the old hole) is closed.
func TestValidateRoleAttrs(t *testing.T) {
	t.Parallel()
	safeApp := roleAttr{exists: true}
	safeMaint := roleAttr{exists: true, bypass: true}
	if err := validateRoleAttrs(safeApp, safeMaint); err != nil {
		t.Fatalf("the safe role pair was rejected: %v", err)
	}
	for _, c := range []struct {
		name       string
		app, maint roleAttr
	}{
		{"app missing", roleAttr{}, safeMaint},
		{"app bypasses rls", roleAttr{exists: true, bypass: true}, safeMaint},
		{"app is superuser", roleAttr{exists: true, super: true}, safeMaint},
		{"maint missing", safeApp, roleAttr{}},
		{"maint lacks bypass", safeApp, roleAttr{exists: true}},
		{"maint is superuser", safeApp, roleAttr{exists: true, super: true, bypass: true}},
	} {
		if err := validateRoleAttrs(c.app, c.maint); err == nil {
			t.Errorf("%s: unsafe roles were accepted", c.name)
		}
	}
}

// TestAssertPoolPrincipalRejectsSuperuser is the runtime half: a raw pool that
// never assumed a role is the DSN superuser, and the effective-principal check
// must reject it — that raw superuser pool is exactly the fail-open the scoped
// pools would have been. The bound pools pass.
func TestAssertPoolPrincipalRejectsSuperuser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svcIface, err := Open(ctx, dsn, WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/core.substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svcIface.Close() })

	raw, err := sql.Open("pgx", dsn) // no SET ROLE: the DSN superuser
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if err := assertPoolPrincipal(ctx, raw, roleApp, false); err == nil {
		t.Fatal("assertPoolPrincipal accepted a raw superuser pool; it must reject one")
	}

	app, err := openPool(dsn, "_probe", roleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	if err := assertPoolPrincipal(ctx, app, roleApp, false); err != nil {
		t.Fatalf("the substrate_app pool was rejected: %v", err)
	}

	maint, err := openMaint(dsn, roleMaint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = maint.Close() })
	if err := assertPoolPrincipal(ctx, maint, roleMaint, true); err != nil {
		t.Fatalf("the substrate_maint pool was rejected: %v", err)
	}
}

// TestOpenFailsClosedWithoutSafeRoles is the end-to-end proof, on its OWN
// cluster so the bound roles are genuinely absent: a DSN that BYPASSES row
// level security but cannot create roles (the production hazard — a
// superuser-ish DSN where role creation is denied) must make Open REFUSE by
// default, and proceed only under the explicit dev escape hatch.
func TestOpenFailsClosedWithoutSafeRoles(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test in -short mode")
	}
	ctx := context.Background()
	c, err := postgres.Run(ctx, "pgvector/pgvector:pg16",
		postgres.WithDatabase("substrate"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(120*time.Second)))
	if err != nil {
		t.Fatalf("start a dedicated container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	base, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	for _, ext := range []string{"vector", "pgcrypto"} {
		if _, err := admin.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS `+ext+` SCHEMA public`); err != nil {
			t.Fatalf("create extension %s: %v", ext, err)
		}
	}
	// The hazard: bypasses RLS, but NOCREATEROLE, so the substrate roles never
	// get made and the scoped pools would run as this bypassing role.
	if _, err := admin.ExecContext(ctx,
		`CREATE ROLE appbypass LOGIN PASSWORD 'pw' BYPASSRLS NOCREATEROLE NOSUPERUSER`); err != nil {
		t.Fatalf("create appbypass: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `GRANT CREATE, USAGE ON SCHEMA public TO appbypass`); err != nil {
		t.Fatalf("grant schema: %v", err)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("appbypass", "pw")
	dsn := u.String()

	if _, err := Open(ctx, dsn, WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/core.substrate.reamde.dev")); err == nil {
		t.Fatal("Open succeeded with no bound roles and a bypassing DSN; it must fail closed")
	}
	// The explicit escape hatch downgrades the refusal to a warning.
	svc, err := Open(ctx, dsn, WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/core.substrate.reamde.dev"), WithInsecureAllowSuperuser())
	if err != nil {
		t.Fatalf("the escape hatch did not let a dev database proceed: %v", err)
	}
	_ = svc.Close()
}
