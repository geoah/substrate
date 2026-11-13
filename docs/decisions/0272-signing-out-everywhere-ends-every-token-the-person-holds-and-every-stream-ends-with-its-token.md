---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0272. Signing out everywhere ends every token the person holds, and every stream ends with its token

## Context and Problem Statement

[0143](0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)
kept sign-in tokens open-ended and named revocation as the control that
works, and recorded the cost: "a leaked login token never stops working on
its own, and a password change does not end it. The holder can also mint new
tokens ..., so recovering from a leak means deleting every token minted
since". A full token can also mint an assistant's token and approve an app,
`full` access included. And only an app's open stream was cut when its token
was revoked (decision 0195); a full token's watch or chat stream ran on. The
proposal is F5 of [pull request 1313](https://github.com/geoah/substrate/pull/1313),
row 32: sign out everywhere, offered when a password changes; every stream
cut when its token dies; sign-in tokens stay non-expiring.

## Considered Options

- Expire sign-in tokens.
- Sign out everywhere ends sign-in and minted tokens, and leaves an app's
  and an assistant's for the owner to revoke one by one.
- Sign out everywhere ends every token the person holds, and every stream is
  cut with its token.

## Decision Outcome

Chosen: sign out everywhere ends every token the person holds, and every
stream ends with its token, because every kind of token is one a leaked full
token could have made, so a sign-out that left any kind would leave the leak
a way back.

`POST /signout/everywhere` takes both current factors in the body and no
token, as the credential changes do, and `POST /password` does the same
with `"signOutEverywhere": true` in the request that changes the password,
since the code it carries is spent once. It deletes every live token record
the person holds, in one transaction: a token from `/login` or `/register`,
one minted through `POST /tokens`, an assistant's for the MCP door, and an
app's from the authorization flow. The person is `substrate.PersonOf`, the
repository's one sign-in today. Every stream reads its token again whenever
the repository's changelog moves, which a revoke and a sign-out do, through
the token cache a token write empties, and ends with an `auth` error: a
watch on the records route or the change feed, a chat's stream and a
thread's events, whatever kind of token opened it. A token is minted
standing on the token that asked for it (`POST /tokens`) or approved it (an
app's code), read again inside the mint's transaction, which a sign-out's
transaction excludes, so a request authenticated before the sign-out, or a
code approved before it, mints nothing after it. Sign-in tokens keep no
expiry.

Expiring sign-in tokens was rejected for 0143's reasons, which stand: the
console and every stored CLI context would sign out on a schedule, and a
leaked token mints an open-ended one before it lapses. Leaving apps' and
assistants' tokens was rejected because a leaked token can approve an app
with `full` access or mint an assistant's token, and the person signing out
after a leak would not know to look.

### Consequences

- Good, because one action, with the factors, takes the account back from a
  leaked token and everything it made, and every stream it opened ends at
  once.
- Good, because a full token's streams end with their token, as an app's
  always did.
- Bad, because signing out everywhere signs out every app and assistant
  too, and each asks to be approved again; the browser that asked signs in
  again as well.
- Bad, because the password change and the sign-out are two writes: a
  sign-out that fails after the password changed says so, and the person
  runs it again with the new password; a password write in doubt runs no
  sign-out and says the password may have changed, so the person tries the
  new one, then the old.
- Bad, because a stream that reads its token on every changelog move costs a
  read of the token cache per move, and a database read per stream after a
  token write empties it.

### Confirmation

`TestAStreamEndsWithItsRevokedToken`, `TestSigningOutEverywhereEndsEveryToken`,
`TestAPasswordChangeCanSignOutEverywhere` and
`TestAMintWhoseBodyArrivesAfterTheSignOutMintsNothing` (`internal/testenv`)
hold it over HTTP against the real engine; `TestAMintStandsOnTheTokenThatAskedForIt`
(`internal/engine`) and `TestAnAppsCodeIsExchangedStandingOnTheTokenThatApprovedIt`
(`internal/api`) hold the mints.

## More Information

Amends 0143's Bad consequence on a leaked login token, and its statement
that revocation is the control, for the sign-out half only; 0143's other
half, that a token without scopes has full access, is unchanged here. Decision
0195's lease on an app's stream now covers every token.
