# Plan: the iPhone apps on the public App Store

Status: draft 3, 2026-11-04: draft 2 revised after one review (record at the
end), then the tool chosen (§4) and the review server stood up (§6.1).
Builds on the TestFlight release (`mise run apps:testflight`,
[apps/README.md](../../apps/README.md#testflight)).

## 1. The request

Every app with an iPhone UI goes to TestFlight on each release. The owner
wants them on the public App Store too, with what a listing needs
(descriptions, screenshots, the review's details), kept in this tree and
pushed by a task the way the builds are, using an existing tool where one
fits and building a small one where it does not. The first submission is
Tasks; this plan builds what Tasks needs and names what comes after.

## 2. Where things stand

- Each app has an App Store Connect record (`substrate-<id>` SKUs), an
  internal TestFlight group and TestFlight test information. The records were
  made by hand; the API cannot make them.
- Builds are uploaded by `apps:testflight`, signed by hand with the team's
  Apple Distribution certificate and App Store profiles the API makes, since
  an API key may not use the cloud-managed certificate.
- Each record has an iOS version in Prepare for Submission, set by hand on
  2026-11-04 to the release's version with that release's build attached, so
  App Store Connect shows the app's icon. Each record also has an empty
  macOS version, made when the record was created with both platforms;
  nothing ships for the Mac through the store (§9).
- Nothing else of a listing exists: no description, keywords, URLs,
  category, age rating, screenshots, review details or privacy answers.
- No privacy policy is published anywhere, and the store requires one for
  every app.

## 3. What a submission needs, and who fills it

| What | Where | By |
| --- | --- | --- |
| Name, subtitle, privacy policy URL | app info localization | API |
| Category, content rights declaration | app info, app | API |
| Age rating answers (including the user-generated and AI content questions) | age rating declaration | API, checked by hand the first time: its fields change with Apple's questionnaire |
| Description, keywords, promotional text, support and marketing URLs, release notes | version localization | API |
| Screenshots (§4.3): iPhone 6.9" (1320×2868) or 6.3" (1206×2622), and iPad 13" (2064×2752) for an app that runs on iPad | screenshot sets on the version localization | API: reserve, the upload operations, commit with the MD5 checksum, then order the set |
| Build, version string, copyright, release type (manual) | the version | API |
| Review contact, demo sign-in, notes | review detail | API |
| Price (free), territories, iPad-on-Mac and Vision Pro availability | price schedule, availabilities, app | API or by hand, once |
| App Privacy (data collected and how it is used) | the record's App Privacy page | by hand, once per app: no public API |
| Submitting for review | review submission and its items | by hand, in the website, until one app has passed |

Every app's iPhone UI also runs on iPad (`TARGETED_DEVICE_FAMILY` "1,2")
except Workouts, so every listing but Workouts' needs an iPad set too.

## 4. The tool

**`asc`** ([rorkai/App-Store-Connect-CLI](https://github.com/rorkai/App-Store-Connect-CLI),
MIT, Go): one binary, pinned in `.mise.toml` through mise's GitHub backend
(`github:rorkai/App-Store-Connect-CLI[rename_exe=asc]`, the release's asset
is named per version), at a release at least two weeks old (5.16.0 when
this was written; it releases almost daily). It was tried against the
team's records with the release's API key: `ASC_KEY_ID`, `ASC_ISSUER_ID`,
`ASC_PRIVATE_KEY_PATH` and `ASC_BYPASS_KEYCHAIN=1`, so it reads the same key
`apps:testflight` does and stores nothing in a keychain; `metadata pull`
wrote a listing as one JSON file per locale. What it covers that this plan
needs:

- `asc metadata init|pull|plan|apply --dir`: the texts (name, subtitle,
  privacy policy URL; description, keywords, promotional text, support and
  marketing URLs, what's new) from files, with a dry run;
- `asc screenshots upload --app --version --locale --device-type --path
  --replace`;
- `asc categories`, `asc age-rating`, `asc review` (contact, demo sign-in,
  notes), `asc versions update --copyright`, `asc pricing`.

Its telemetry is on by default: every call runs with `DO_NOT_TRACK=1`. Its
`web` commands use an Apple web session and private endpoints; none is used.

Considered and not used: fastlane's `deliver` and `snapshot` (Ruby, which
nothing in this tree runs, and an Apple ID session for what the API lacks,
which is App Privacy, by hand anyway); extending `appstoreconnect.swift`
(a few hundred lines for the age rating's fields and the screenshot upload
operations, which `asc` already has). `appstoreconnect.swift` keeps what it
does for `apps:testflight`: the records check and the App Store profiles.

### 4.1 The listing, in the tree

`apps/<app>/store/`, in the layout `asc metadata pull` writes (one JSON file
per locale for the app info and for the version), English only for now,
beside `store/review.json` (category, content rights, copyright, age rating
answers, review notes) and `store/screenshots/{iphone,ipad}/`. The name comes
from app.json and is checked against the listing's. `lint:apps` holds the
files' shape and Apple's limits (subtitle 30 characters, keywords 100 bytes,
description 4000, promotional text 170). Nothing in them names a deployment:
URLs are the site's, written as `{site}/…` and filled from
`SUBSTRATE_APPLE_SITE_URL`, and the review server's address and sign-in come
from the environment (§6.1).

### 4.2 The task

`mise run apps:store <app>` runs `asc` to make App Store Connect's editable
iOS version of one app match its listing, and says what it changed:

1. writes the texts, URLs, category, content rights, copyright, age rating
   and review details where they differ;
2. uploads the listing's screenshots, replacing a set whose images differ
   from the ones there (App Store Connect keeps each image's MD5);
3. with `--build <number>`, sets the version string to that build's and
   attaches it; without it, leaves the build alone, so a version a person is
   fixing after a rejection is never given another build behind their back;
4. stops with a message, changing nothing, when the version is not editable
   (in review, or pending release);
5. never submits.

The review sign-in comes from `SUBSTRATE_APPLE_REVIEW_SERVER`,
`_USER` and `_PASSWORD`, never the tree.

### 4.3 Screenshots

**Captured from the simulator.** The fixtures' gallery draws with AppKit on
the Mac (`OffscreenDrawing` hosts views in an `NSHostingView`), so its
pictures have the Mac's controls, fonts and no iPhone chrome: they would
show the app as it never looks on a phone (guideline 2.3.3). The store's
screenshots are taken on simulators instead:

- `mise run apps:store:screenshots <app>` starts a local substrate
  (`dev:up`), seeds it with the app's demo records (a fixture file per app,
  written through the API), boots an iPhone 17 Pro Max and an iPad Pro 13"
  simulator, sets a clean status bar (`simctl status_bar override`: 9:41,
  full battery and signal), installs a debug build signed in to that server,
  and runs the app's `StoreScreenshots` UI test, which walks to each screen
  the listing names and saves it;
- the PNGs are written without an alpha channel, at the device's native
  size, into `apps/<app>/store/screenshots/{iphone,ipad}/`, committed, and
  reviewed as images in the pull request that changes them;
- the gallery stays what it is: a check in pull requests.

Captions and device frames are left out: Apple accepts bare screens, and a
frame is a design choice with no owner yet (§10).

## 5. The icons on the Apps page

Done by hand on 2026-11-04 (§2). From then on, attaching a release's build
is `apps:store <app> --build <n>`, run by a person preparing a submission.
Keeping the icon current on every release, from CI, waits until one app has
passed review (§8).

## 6. What review will ask, and the decisions it needs

### 6.1 A server to sign in to

None of the apps does anything without a substrate. App Review signs in
from Apple's network, so the server must be public, not reachable only on a
private network, and must:

- answer the app's approval page in the in-app browser with a review owner
  and **no second factor**;
- hold synthetic demo records for each app under review, and nothing else;
- be named in the review notes with step-by-step sign-in (the address, the
  owner's name and password, what to tap), with a link to a short screen
  recording of it;
- be reset to its demo data on a schedule, so one review's edits do not meet
  the next.

The notes also say: what a substrate is, with a link to the public
documentation on running one, so the app is not judged unusable without a
server (2.1, 4.2); that the app is a client for a server its user runs; and
that accounts live on the user's server and are never created in the app,
so in-app account deletion (5.1.1(v)) does not apply.

Decided 2026-11-04: a substrate of its own, fresh on every start and
restarted nightly, with one review owner registered by the stack and no
other data; a reviewer's sign-in installs the app's package through its
approval. Its address, the owner's repository name and password are the
release environment's (`SUBSTRATE_APPLE_REVIEW_*`), never this tree.

### 6.2 The privacy policy

Published on the publisher's own website, not the apps' site, written from an
audit of what the apps send (2026-11-04): what is entered and what the apps
read on the device goes only to the substrate the user signs in to, and the
publisher receives none of it. It lists every other recipient by name: Sync
sends a visited place's coordinates to Apple Maps' reverse geocoder for its
name and address; Workouts' Hevy import reads from Hevy with a key it does
not keep; a domain sign-in makes DNS lookups; a build installed from the
download page checks the release feed; Apple sends opt-in crash reports. It
also says the review server holds invented data only. Its URL is the
release environment's (`SUBSTRATE_APPLE_PRIVACY_URL`), never this tree.

Guideline 5.1.1(i) wants the policy reachable inside each app as well as in
the listing. The kit's settings gain a Privacy Policy row in their About
section, opening the URL a release stamps into the build (as it stamps the
site's), so every app on the kit has it; a local build shows none.

A change that sends anything new off the device changes the policy first.

### 6.3 App Privacy answers

Each app answers **Data Not Collected**: Apple counts data as collected when
it leaves the device where the developer or a third party can reach it, and
a server the user runs is neither. The answer holds only while the
publisher's own servers hold nobody's data but the owner's and the review's
synthetic records. If the publisher ever offers a server to other people,
the answers and the policy change first. **Decision for the owner, blocking
before Workouts and Sync:** confirm that.

### 6.4 Which apps go first

| App | Review risk | Recommendation |
| --- | --- | --- |
| Tasks | Low: a plain client | First |
| Workouts | HealthKit: needs the policy, and its usage strings must say what it reads and writes | Second |
| Chat | Medium: AI-generated content (the age rating questions, and 1.2's content controls) and microphone and speech access | Third, after its age rating is settled |
| Sync | High: Health, contacts, calendar, reminders and location under Always authorization (visit monitoring, no `location` background mode) in one app. Its Health write string describes a debug feature, which a release build must not ship | Later: fix the string and request no Health write in release builds; the notes explain visit monitoring (5.1.5) |
| Console | High: a bundled web app in a shell (4.2, minimum functionality), with unrestricted web content for the age rating | TestFlight only |

## 7. Submitting

By a person, in App Store Connect, for the first submissions. Release type:
manual, so a passed review waits for the owner. Version numbers stay the
release's (0.x); the store accepts them. A `--submit` flag is later work
(§10).

## 8. The work, in order

1. The privacy policy (published on the publisher's website, §6.2), and the
   kit's Privacy Policy row.
2. The review server (§6.1), decided and seeded.
2a. A privacy manifest (`PrivacyInfo.xcprivacy`) in every app target and
   extension, declaring the required-reason APIs each uses (UserDefaults,
   file timestamps, disk space, boot time) and that it tracks nothing and
   collects nothing for the developer; App Store Connect refuses an upload
   that uses one of those APIs without a manifest (ITMS-91053).
3. `asc` pinned in `.mise.toml`, and `apps:store` over it for the texts,
   review details and `--build`; Tasks' listing.
4. Screenshots: `apps:store:screenshots` and Tasks' `StoreScreenshots` UI
   test, iPhone and iPad; their upload in `apps:store`.
5. By hand: App Privacy, the age rating checked, price and territories; then
   Tasks' submission.

After Tasks passes:

6. Workouts and Chat: their demo records, UI tests and listings.
7. CI: the build attached to the editable version after each release once
   it is processed, and a scheduled `apps:store --check` that reports a
   listing edited in the website.

## 9. What this plan does not do

- The Mac apps stay on Sparkle and the feeds. The empty macOS versions are
  left as they are: an unsubmitted version costs nothing.
- Android's store (Play) is its own plan.
- Localisation beyond English.

## 10. Later

- Captions and frames on the screenshots, once someone owns the look.
- `apps:store --submit`.
- App previews (video).
- External TestFlight testing, which needs the same review details.

## Review record

**Draft 2, 2026-11-04.** One review (Opus 5.5). What changed:

- Screenshots are captured from simulators, not drawn from fixtures: the
  gallery draws with AppKit on the Mac, so its pictures are not what the
  app looks like on a phone (2.3.3), carry an alpha channel the store
  refuses, and are 1179×2556, not a store size (§4.3).
- iPad sets are required for every app but Workouts, which alone is
  iPhone-only; iPad-on-Mac and Vision Pro availability are decided once
  (§3).
- The build is attached only on `--build`, never by CI on every release,
  so a version being fixed after a rejection keeps its build; CI, the
  weekly check and keeping the icon current wait until one app has passed
  (§4.2, §5, §8).
- The tool (after the review): `asc`, tried against the team's records,
  in place of extending `appstoreconnect.swift` (§4).
- No deployment is named: the listing's URLs are the site's, read from the
  environment, and the review server comes from the environment (§4.1).
- The review server's requirements: public, no second factor, synthetic
  data, reset on a schedule, step-by-step notes with a recording, the
  documentation link, and the account deletion note (§6.1).
- Chat's risk rises for AI-generated content; Sync's Health write string
  describes a debug feature and must not ship; its location is visit
  monitoring under Always authorization (§6.4).
- Data Not Collected holds only while the publisher's servers hold nobody
  else's data; the owner's confirmation blocks Workouts and Sync (§6.3).
- The content rights declaration, copyright and the age rating's changing
  fields were missing (§3); `--submit` moves to later (§7, §10).
