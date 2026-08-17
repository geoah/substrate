# Security policy

## Reporting a vulnerability

Report it privately, through GitHub:
<https://github.com/geoah/substrate/security/advisories/new>. That opens a
draft advisory only you and the maintainer can read, and it needs no email
address from either of us. If you cannot use GitHub, mail
<george@noodles.gr>, the address on the commits in this repository.

Do not open a public issue for a vulnerability.

Say which version or commit you tested, how you reached the bug, and what an
attacker gets. A reproduction against `docker compose up` is the fastest route
to a fix.

## What to expect

This is one person's project, so the numbers below are targets, not a
contract: a first reply within 7 days, and an assessment of whether it is a
vulnerability within 14. If 7 days pass in silence, mail the address above,
because a GitHub notification may have been missed.

Pre-1.0, the only supported version is the newest release plus the `main`
branch. Fixes land on `main` and go out in the next release; nothing is
backported. An advisory is published when the fix ships, and it credits you
unless you ask it not to.

## In scope

- **Cross-repository access.** One user's token reading or writing another
  user's repository, or any bypass of the row level security that binds the
  `substrate_app` role.
- **Authentication bypass.** Registering without the invite code, minting or
  using a token without the password and the TOTP code, or changing a
  credential without the current second factor.
- **Secret material escaping.** `SUBSTRATE_CREDENTIAL_KEY`, a sealed value, a
  provider OAuth token or a changelog signing seed reaching an API response, a
  log line, another repository, or a record any token can read.
- **Sandbox escape under `SUBSTRATE_SANDBOX=enforce`.** A function body
  reaching the filesystem, the network or another process past what
  [the sandbox](docs/functions.md#the-sandbox) says it may.
- **Remote code execution and SQL injection**, anywhere.
- **Forging the chain.** An entry that `repository verify` accepts although its
  bytes, its hash or its signature do not agree, on a repository whose signing
  has activated.

## Not a finding

- **The shipped compose defaults.** `compose.yaml` is a laptop quick start and
  says so: the invite code is `let-me-in`, the Postgres password is
  `postgres`, no `SUBSTRATE_CREDENTIAL_KEY` is set, and
  `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES=true` runs the changelog
  unsigned. Each is a deliberate default with a comment naming what to change
  before the deployment is reachable by anyone else. A report that they are
  insecure adds nothing.
- **Anything a `SUBSTRATE_INSECURE_*` variable turns on.** Those switches exist
  to weaken the substrate for local testing, they announce themselves at boot,
  and `SUBSTRATE_INSECURE_DISABLE_TOTP` in particular makes a password the
  whole credential on purpose.
- **What the host operator can do.** The operator holds the database and the
  credential key, so the chain and its signatures are tamper evidence and never
  evidence against the operator ([the chain](docs/changelog.md#the-chain)).
  Operator commands run on the box, over the DSN, by design.
- **A token doing what a token may.** A token has full access to its own
  repository: there are no scopes and no roles, and function bodies a user
  installs run with the user's data in reach.
- **Documented absences.** No sharing, no second user reading your repository,
  no erasure or retention policy, one replica only. Each is written down in
  [running a substrate](docs/operations.md) as a decision.
- Findings from a scanner with no reachable attack behind them: a missing
  hardening header, a TLS setting on a proxy this repository does not ship, a
  dependency advisory the code never calls.
