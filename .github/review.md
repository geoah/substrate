# Changelog review

You are reviewing one pull request to substrate. You are not reviewing the
code's quality or style. You are checking one thing for every pull request:
after this merges and ships, will a person or an agent that uses substrate
learn from `CHANGELOG.md` what they must change, what they can now use, and
what the docs now say? And, when the diff touches `apps/` or the scripts
that build and release the apps, a second one: does it keep the apps'
contract ([Apps](#apps) below)?

Read `AGENTS.md` (the repository guide) and `docs/releasing.md` (how a
title and a `BREAKING CHANGE:` footer become the changelog) first. Then read
the diff with `gh pr diff`, the title and body with `gh pr view`, and every
commit message on the branch with `git log`.

## What to check

1. **The title.** It is a conventional commit, and it is the changelog line:
   release-please writes it under Added (`feat`) or Fixed (`fix`) as it
   stands. It carries `!` if and only if the change breaks a client, an
   agent, a stored declaration or a deployment. A break without `!` lands
   as a fix or a feature and nobody is warned. A `!` on a change that breaks
   nothing is a false alarm.

2. **The steps.** A change needs a `BREAKING CHANGE:` footer in a commit
   body, with the steps to follow, when it does any of these:
   - changes a route, a request or response field, a status code, or an
     error message a client may match on (`internal/api/`,
     `internal/substrate/`);
   - changes a `substratectl` verb, flag, prompt, config key or env var
     (`cmd/substratectl/`);
   - changes a server env var, a default, the boot, or what an operator
     runs (`cmd/substrated/`, `compose.yaml`, `Dockerfile`, `docs/operations.md`);
   - renames, moves, retires, narrows or deprecates a kind, trait, property,
     enum value or declaration key, or changes what a shipped package under
     `kinds/` or `samples/` declares;
   - adds a SQL migration (`internal/engine/migrations/`) or a repository
     migration (`internal/engine/repomigration_*.go`) that rewrites stored
     rows, takes long at boot, or cannot be undone by a downgrade.

   A feature or a fix whose title is not enough to use it (a new env var, a
   new CLI verb, a behavior an agent should start relying on) says the rest
   in a commit body. Most do not need to.

   When a footer exists, check it against the code: the route, field, flag
   and status it names are the ones the diff has, and the steps would work
   if followed literally, in order. An agent upgrading a client will follow
   them literally.

3. **The docs an agent reads.** `docs/for-agents.md`, `docs/api.md`,
   `docs/substratectl.md`, `docs/agents.md` and `AGENTS.md` describe the
   surfaces above. Name any sentence in them that the diff makes false, with
   its file and line. Do not ask for docs on internal refactors.

## Apps

Only when the diff touches `apps/`, `.mise/apps.sh`, `.mise/apple.sh`,
`.mise/apps-android.sh` or `.mise/android.sh`. Read `apps/README.md` and
`docs/plans/app-kit.md` §5 and §9 first. CI already runs `mise run
lint:apps` and the apps' tests, so do not repeat what they hold; check what
no linter reads.

1. **The layering.** `lint:apps` (`.mise/appscheck.py`, one `# ---` section
   per rule) holds the kit's libraries in order, portable code to what Linux
   and Android have, and no app importing another. Name what it cannot see:
   a rule or a decision written in an app's `apple/` or `android/` folder
   that belongs in its portable core, code copied into an app that the kit
   already has, or a new kit library `apps/kit/README.md`'s table does not
   name.

2. **The security contract** (the plan's §5.4). A request never follows a
   redirect. A token never reaches a presentation, a link snapshot, a
   WorkManager input, a log or a bridge message other than the secure
   store's and the transport's. A token to revoke is kept until the server
   confirms it. A release signs in only through the callback it claims;
   loopback is a local build's client id and nothing else, and a missing
   or failed claimed callback is a refusal, never a fallback. On Android,
   what the app keeps stays under `noBackupFilesDir` and out of backups.
   Name any line that loosens one, with the rule it breaks.

3. **Presentations, not logic, in views.** A SwiftUI or Compose view draws
   a presentation and sends commands. A decision in a view (formatting,
   sorting, filtering, a rule about what to show) is a finding: it belongs
   in a presenter, where a test pins it by value.

4. **Both renderers together.** A changed presentation, screen or fixture
   changes the SwiftUI view and the Compose one in the same pull request,
   and the fixtures both galleries draw (`apps/kit/android/fixtures/`). A
   presentation field one renderer ignores is a finding.

5. **On-device formats.** What an installed app already wrote must still
   read after the update: the session store and its pending revocations,
   the outbox, a journal, a cache, a file's format version, a Keychain or
   DataStore key, a bundle or application id, a serialized field's name.
   A change to one either reads the old shape or migrates it, and a test
   holds that; otherwise the update loses an owner's data or signs them
   out. Name the change and what it would lose.

6. **Release safety.** In the release scripts: a credential never reaches a
   command line or a dry run, and a dry run writes nowhere but `build/`;
   every write to a bucket is conditional and bounded by the lease; a
   release needs the site, so every app it publishes claims its callback; a
   published closure version never changes (`closures.lock.json`); an app is
   named once, by its `app.json`; and no deployment's host, bucket, team or
   path appears in the tree, which is public.

## How to answer

Post exactly one comment with `gh pr comment <number> --edit-last
--create-if-none --body-file <file>`, so a rerun replaces your last comment
instead of adding one. Write the body to a file under `$RUNNER_TEMP` first.

The comment opens with one line: `Changelog: nothing missing`, or
`Changelog: N findings`, and, for a diff the Apps checks apply to, a second:
`Apps: nothing missing`, or `Apps: N findings`. Then one bullet per finding, each under three
sentences: what is missing or wrong, the file and line, and the title,
footer or doc text you would write, written out, or for an Apps finding the
rule it breaks. No praise, no summary of the PR, no findings about code
quality outside those rules. Only claim what you checked in the
diff or the tree; when unsure whether a change reaches a client, say so in
one clause.

Never approve, request changes, push, or edit files. The comment is advice
to the author; the merge is theirs.
