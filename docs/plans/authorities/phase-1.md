# Authorities phase 1: status

As of 2026-11-02. Phase 1 of [the plan](../authorities.md) is implemented
in five pull requests, and none is merged. This page is the handoff: what is
open, the merge order, what review found, and what is left.

## Pull requests

| PR | Branch | Base | Head | What |
| --- | --- | --- | --- | --- |
| #1146 | `feat/domains-record` | `main` | `ccea7beb9` | `internal/domains` reads the `_substrate` TXT record; `authorize.ConfinedClient` |
| #1148 | `feat/verify-domain` | `feat/domains-record` | `cb36c3403` | **Verify domain**, the server instance id, `login_hint`, `substratectl domain`; record 0208 |
| #1149 | `feat/verified-apps` | `feat/verify-domain` | `5e9994bb8` | verified apps, the `app:<authority>` and `app:local` actors, `SUBSTRATE_INSECURE_AUTHORITY_DOCS`; record 0209 |
| #1147 | `feat/own-authority-packages` | `main` | `1911b55d4` | a token creates packages only under its repository's authority (a break), `SUBSTRATE_INSECURE_ALLOW_FOREIGN_PACKAGES`; record 0210 |
| #1151 | `feat/apple-claimed-callback` | `main` | `4840a7ba8` | Apple apps: claimed `https` callback, finding a server from a repository name, `apple:site` |
| #1157 | `integrate/authorities-phase1` | `main` | `c93f72474` | draft, never merged: all five combined, for CI and end-to-end testing |
| #1145 | `docs/authority-documents` | `main` | | the plan and this page |

Issue #1144 tracks securing code installed from outside authorities.

## Merge order

#1146, then #1148, then #1149: they are stacked. #1147 and #1151 can merge
at any point. Whichever lands after the stack conflicts in
`docs/decisions/README.md`, `docs/operations.md` and
`internal/config/config.go`. Each of those conflicts keeps both sides, as
commit `1785d7928` on #1157 shows. Records 0208, 0209 and 0210 are
`proposed`.

## Checks run

- CI passes on #1146, #1147, #1148 and #1149. On #1151, `providers e2e`
  failed while restoring the Go module cache (`tar: Cannot open: File
  exists`), before any test ran: rerun it. CI on #1157 was still running.
- On #1157, locally: `go build ./...`; the Go tests of every package the
  five pull requests touch; the 15 engine database tests they add or change;
  `mise run lint`, `fmt:check`, `kinds:check` and `commits:check`; console
  lint, format, typecheck and all 3718 console tests. All pass.
- `internal/e2e`, which CI does not run: 11 of 65 cases fail on `main`, and
  the same 11 fail on #1147. They are STORY-03 to STORY-06, REF-04, LOG-04,
  FN-03, AGN-02, CAL-02, OCC-01 and ERR-04. No issue is filed.
- #1151 at `4840a7ba8`, on a Mac: every Apple build and test passes, and the
  four hand checks pass on the iOS simulator. The macOS apps were built and
  not driven by hand.

## Review record

Each pull request but #1146 had one Codex review. Every finding was fixed.
The fixes were not reviewed again.

| PR | Findings | Fixed in |
| --- | --- | --- |
| #1147 | a quarantined package of another authority could not be repaired | `1911b55d4` |
| #1148 | a forged `Host` header verified a name without DNS; an echoed instance id passed the server check; full-access app tokens could verify; two first boots could mint two instance ids; record 0208 promised a re-check no code made | `cb36c3403` |
| #1149 | removing the DNS record did not invalidate a pending approval; loopback tokens lost their old key namespace; the document override was not limited to development | `47479f492`, rebased as `5e9994bb8` |
| #1151 | a failed DNS lookup fell through to the web host; a rejected token was forgotten when its revoke failed | `c304cc965`; the Mac build found DNS-SD reporting SERVFAIL as no record, fixed in `4840a7ba8` |

## Choices to confirm

These were made while implementing or fixing, and the plan did not settle
them:

- A verified app's redirect may sit off its `client_id`'s origin, on the
  authority or a name under it. This relaxes 0194's origin rule.
- `SUBSTRATE_INSECURE_AUTHORITY_DOCS` and
  `SUBSTRATE_INSECURE_ALLOW_FOREIGN_PACKAGES` are not limited to
  development, as with the other `SUBSTRATE_INSECURE_` settings. The first
  is disclosed on the approval page and on the token.
- A full-access app token may verify a domain, because 0204 makes `full` a
  sign-in.
- Engine tests run with the package rule lifted (`OpenForTest`); only the
  rule's own tests turn it on.
- A token keeps the actor it was minted with until the app is approved
  again: the set up check does not verify the publisher again.
- The Apple apps confirm a negative DNS answer through libresolv, which adds
  up to about 4 seconds before a failed lookup shows its error.

## Left to do

- **Owner:**
  - merge the pull requests and accept records 0208 to 0210;
  - add `_substrate.substrate.reamde.dev TXT "v=substrate1 doc=https://substrate.reamde.dev/.well-known/substrate/authority.json"` to the `reamde.dev` zone;
  - serve a static site at the root of `substrate.reamde.dev`;
  - turn on Associated Domains for each App ID;
  - provide `SUBSTRATE_APPLE_SITE_URL`, `SUBSTRATE_APPLE_TEAM_ID` and the upload key;
  - set `SUBSTRATE_PUBLIC_URL` on production;
  - then run **Verify domain** for existing repositories.
- **On devices:** the claimed callback on iOS and macOS; libresolv on a
  physical iPhone over a VPN, an encrypted DNS profile and iCloud Private
  Relay, which the simulator cannot show; the macOS apps by hand.
- **Real DNS, end to end:** a repository verified against a public server,
  and an app verified against a published record and document.
- **Tests:** review the fix commits; run `internal/e2e` on #1157; rerun
  #1151's `providers e2e`.
- **Later phases:** phases 2 and 3 of the plan. That includes a way for 0207's app agents to ship in a published package, and replacing the foreign-packages setting with local documents.

## Briefs

[briefs/](briefs/) holds the work-package briefs the implementing agents
were given, as given. Later review decisions replace parts of them: the
own-host rule, the document override's limits, and what a failed lookup
does.
