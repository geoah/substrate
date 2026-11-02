# Work package D: the Apple apps sign in through a claimed https callback and find a server from a repository name

Start from main: `git fetch && git switch -c feat/apple-claimed-callback origin/main`. Open the PR as a **draft** with base `main`.

This machine has no Xcode, so you cannot build or run Swift here. Write the code and its tests carefully. The PR body ends with a section "Build and verify on a Mac" giving the exact commands a Mac agent runs (read `.mise/apple.sh` and `apps/apple/README.md` for the tasks), and what to check by hand on a device.

1. **SubstrateKit sign-in** (`apps/apple/SubstrateKit`). When the app's manifest names an https redirect, open `ASWebAuthenticationSession` with `callback: .https(host:path:)`; every target is iOS and macOS 26, past the 17.4 and 14.4 minimum. Keep the loopback path for local builds (each app's `localClientID`). Check `state`, check `iss` (RFC 9207; the server sends it) against the server the app meant, and check that the token response's `repository` is the name the app resolved.
2. **The connect screen** takes one field, a server or a repository name, and resolves it in the plan's order (decision 1). If it serves `/.well-known/substrate/server.json`, it is a server. Otherwise read the TXT record `_substrate.<name>` through `DNSServiceQueryRecord` (a small async wrapper), then `https://<name>/.well-known/substrate/repository.json`, then probe the parent host. Send `login_hint=<repository>`. Parse the record by the same rules as `internal/domains/record.go` (`git show origin/feat/domains-record:internal/domains/record.go`): one `v=substrate1` record, `key=value` fields, `server` an https origin. Write Swift tests for the parser and the resolution order with a stubbed resolver and transport.
3. **Manifests and entitlements.** Each app's https client id and redirect come from build settings (the Info.plist keys such as `SubstrateWorkoutsClientID` exist; add the redirect). In `apps/apple/project.yml`, give each app target the Associated Domains entitlement `webcredentials:<site host>` and `applinks:<site host>` when a site host build setting is set, and none for a local build.
4. **`.mise/apple.sh` publishes the site** under `SUBSTRATE_APPLE_SITE_URL`: an https origin on a host of its own, served at its root, because Apple reads the association file from the host's root. It publishes:
   - `/.well-known/apple-app-site-association`: `webcredentials` and `applinks` for every app id, the team id from the environment;
   - `/.well-known/substrate/authority.json`: the authority from `SUBSTRATE_APPLE_SITE_AUTHORITY`, defaulting to the site host; every app with its `client_id`; no packages in phase 1;
   - `/apps/<app>/client.json` for each app: `substrate_authority`, and the redirect `https://<site host>/apps/<app>/callback`.

   The release builds stay where they are. Keep refusing shared hosts as the script does now. Print the `_substrate` TXT record the operator must add, `v=substrate1 doc=<site>/.well-known/substrate/authority.json`. Never put a real host in the tree.
5. **Docs.** `apps/apple/README.md` and `apps/apple/SubstrateKit/README.md`. In `docs/apps.md`'s native-apps section, the sentence that says SubstrateKit has no claimed https callback.

Suggested title: `feat(apple): sign in through a claimed https callback and find a server from a repository name`.
