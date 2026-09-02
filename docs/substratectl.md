# substratectl

`substratectl` is the command-line client: kubectl-shaped, speaking the
[REST surface](api.md). Everything in a repository is a record of a declared
kind, addressed as `{authority}/{kind}/{id}`, and the CLI mirrors that:
kinds, get, apply, patch, delete, edit, watch. It lives in the
same repository as the substrate (`cmd/substratectl`) and builds with Go.

```
substratectl register --server https://substrate.example --username ada
substratectl login --username ada
substratectl kinds                        # every declared kind
substratectl get task                    # list a collection
substratectl get task t9 -o yaml         # one record, apply-able envelope
substratectl apply -f task.yaml           # put: merge, never prune
substratectl patch task t9 --state status=done
substratectl watch                        # resumable change stream
```

## Two hats

`substratectl` addresses a substrate two different ways, and the flags say which.

**The user's hat** speaks HTTP and carries a token: everything above, plus
`register`, `login`, `logout`, `token`, `user password`, `user totp`,
`trigger`, `function` and `bundle`. It needs a server and a token, and it can
run anywhere.

**The operator's hat** speaks to the box's Postgres directly and holds no token
at all: `user reset`, `repository list`, `repository inspect`,
`repository verify`, `repository rebuild`, `repository reembed`,
`repository reseal`, `blobs migrate`. It needs `--dsn` (or
`DATABASE_URL`), and without one
every operator command refuses before touching anything.
[Running a substrate](operations.md) is where that hat lives.

`substratectl version` belongs to neither hat: it prints the client version
and the API version it speaks (`substratectl <version> (api v1)`), and needs
no server and no token.

## Registering

`substratectl register` creates a user on a substrate that is open for registration:
it asks for a TOTP enrollment, prints it once for an authenticator, and hands
back one code with the password you choose. `--totp-secret` brings your own
seed and skips the enrollment call, which is what makes an unattended
registration possible. `--authority` names the DNS-style authority the
repository owns, the home of every kind you declare; omitted, the substrate
names it `<username>.<its own host>`, and `register` prints the result.

Against a substrate that verifies no second factor
([the local TOTP-off switch](auth.md#the-second-factor-can-be-switched-off-locally)),
`register` and `login` ask for no code and `register` skips the enrollment
entirely: `substratectl` reads `GET /.well-known/substrate/server.json` first
and stops asking for something nothing checks.

`substratectl login` presents both factors and mints a token record, which it stores
as the current context. `substratectl logout` revokes that token record and then
forgets it, because a session is its [token record](auth.md#tokens).

`substratectl token create --label backup` mints a token for a script or a device and
prints the secret exactly once; `--expires` takes a duration (`720h`) or an
RFC 3339 instant. `token list` and `token revoke <id>` are metadata and one
delete. `substratectl user password` and `substratectl user totp` change your factors, and
both send **no bearer token at all**: the
[password-factor rule](auth.md#the-credential-and-the-password-factor-rule)
refuses one, so carrying it would only teach the wrong habit.

Every prompt has a flag or a `--*-stdin` twin so the same command scripts
headlessly, and a password is never an argument.

## Configuration

Config lives at `~/.config/substratectl/config.yaml`, mode 0600 in a 0700 directory
(`SUBSTRATECTL_CONFIG` and `XDG_CONFIG_HOME` are respected). It holds one **context**
per substrate: a name, the server, the username, the token, and the token's id
so `logout` can revoke the very token it forgets. **No repository** — the token
implies it, and there is nothing else to configure.

`SUBSTRATE_SERVER` and `SUBSTRATE_TOKEN` override the file, and flags override
both; `SS_SERVER` and `SS_TOKEN` are the one accepted alias, read only when the
canonical variable is unset. `--context` picks a stored context by name.
`--actor` names the [actor](api.md#actors) a write is attributed to, and
defaults to `substratectl`.

## Reading

`substratectl get <plural> [id]` reads a collection or one record. The plural may be
qualified (`tasks.substrate.reamde.dev/task`) or bare (`tasks`), which resolves against
the kind registry; when two installed authorities declare the same plural, that
plural needs qualifying, or `-g` to name the authority (every bundle
installs a `config`, so `configs` always needs one). Lists take `--filter` (the
JSON [filter grammar](api.md#the-filter-grammar)), `-l` label selectors,
`--order-by`, and `--limit`; `--after` resends the opaque keyset cursor a page
printed, and `-w` streams that one collection's changes instead of listing it.

`-o yaml` writes each record as an [envelope](data-model.md#the-envelope)
document (`kind`, `metadata`, `data`, and the server-set `status`),
`---`-separated, and `-o json` writes the same shape. REST returns the record
flat; the envelope is the CLI's YAML form. `status` is ignored on input, so the
output applies back unchanged.

## Writing

- `apply -f FILE` applies YAML manifests (`---`-separated; `-` reads stdin). A
  document with `metadata.id` is put at that id; without one it creates. Apply
  is `put`: it **merges and never prunes**; deletion is only ever the explicit
  `delete` verb. Vocabulary documents apply too ([vocabulary](vocabulary.md)): they ride
  the batch vocabulary verb as one transaction.
- `patch <plural> <id>` edits in place: `--state status=done` for
  [transitions](data-model.md#validation-and-state-machines) (apply cannot
  move a state), `--prop` for properties, `--label` for labels, and `-p` for a
  raw JSON patch, where a null value deletes a key.
- `edit <plural> <id>` opens the manifest in `$EDITOR` and applies what comes
  back.
- `delete <plural> <id>` tombstones; hard deletion waits on finalizers.

A pointer at another record is a property, so `apply` and `patch` write it like
any other value: `--prop project=infra7` against a pinned declaration, or the
full `<kind>/<id>` path where the declaration names no kind.

## Watching, triggers, and bundles

`substratectl watch` streams [the changelog](changelog.md), one line per committed change,
resumable with `--from` and filterable by `--kinds`, `--actors` and `--ops`. To
narrow to one collection instead, `substratectl get <plural> -w` streams that
one kind's changes.

Delivery bookkeeping lives on [triggers](functions.md#triggers), not on
functions, so it is the `trigger` subcommands that drive it: `status` shows
each trigger's kind, callable, cursor, lag, last fire, parked count and, for a
webhook trigger, the public path its `WEBHOOK` column prints
(`/webhooks/<username>/<trigger-id>`, the URL an external service POSTs to);
`parked` lists the deliveries it gave up on and `retry` re-runs one; `replay`
resets a record-sourced trigger's cursor; `run` synthesizes a single delivery;
and `wake` scans a trigger immediately. Trigger rows are ordinary records, so
`get` / `apply` / `delete` edit them like anything else.

`substratectl function call <name> --input <json>` invokes one
[function](functions.md) directly, applies its effects under the function's
actor, and prints the output. There is no build step: a function is inline
source on its manifest, so it installs with the ordinary `apply`.

`substratectl bundle list` / `status` report a [bundle](bundles.md)'s computed
state, and `disable` / `enable` / `uninstall` / `purge` move it through its
lifecycle; install and upgrade are `substratectl apply` of the closure, and `connect`
starts the host [OAuth flow](bundles.md#the-oauth-facility) for an account
record, printing the consent URL.

Next: the [web console](console.md), the same repository in a browser.
