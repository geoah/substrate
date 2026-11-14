---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0288. A short name is completed under the configured public host, and a local name is for development

## Context and Problem Statement

A bare label typed at registration was completed under the host the request
reached, so the name a person got depended on the address their browser
used: `ada` became `ada.localhost` on a laptop and `ada.127.0.0.1` through a
dev proxy, and a server reached under two names gave out names under both
(the 1.0 review, F2, "The default name is whatever address the browser
used"). A name under `localhost` or an IP address means nothing to anyone
else, and was permanent.

## Considered Options

- Complete under the request's host, as before.
- Complete under the server's configured public host, and refuse names under
  `localhost` and IP addresses everywhere.
- Complete under the configured public host, and admit names under
  `localhost` and IP addresses only in development.

## Decision Outcome

Chosen: a short name is completed under the host of `SUBSTRATE_PUBLIC_URL`,
and a new name under `localhost` or an IP address is admitted only where that
host is itself `localhost` or a loopback address, which is what development
is. The request's host is chosen by the caller, so it decides nothing; and
refusing local names everywhere would break the dev tasks and `docker compose
up`, which register `ada` and now set a local public URL.

- With no public URL a short name is refused, saying to type the whole name.
- Finding an existing person or repository by a short name (a sign-in, a
  delete, a rename's current name) still completes it under the request's host where no
  public URL is configured, and is never refused: a repository named under
  `localhost` before this rule keeps its name and can be renamed
  ([0287](0287-a-repositorys-name-is-an-alias-and-a-former-name-holds-for-90-days.md)).
- Discovery states the host (`registration.nameHost`), and the console
  previews names with it rather than with the address it was reached at.
- The rule lives in one place, `vocabulary.NameRule`, so a registration
  rewritten later takes it as it is.

### Consequences

- Good, because the name a person gets no longer depends on how they reached
  the server, and a production server gives out no local names.
- Good, because the dev tasks and `compose.yaml` register `ada` as before.
- Bad, because a server without `SUBSTRATE_PUBLIC_URL` now refuses short names
  at registration, where it used to complete them under the request's host.
- Bad, because development is read off the public URL alone: a server whose
  public URL is a loopback address admits local names for anyone who reaches
  it.

### Confirmation

`TestTheDefaultNameIsCompletedUnderThePublicHost` holds the review's table row
for row, local names refused outside development and admitted in it, and the
lookup that never refuses.

## More Information

Amends
[0274](0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md),
whose sign-in name was completed as a repository's was, under the host the
request reached: a person's name and a repository's are completed by this
rule, a new one at registration and an existing one at every other door.
