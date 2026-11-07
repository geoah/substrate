---
status: accepted
date: 2026-11-07
decision-makers: George Antoniadis
---

# 0237. A second factor is the owner's choice, and sign-in asks for the code only of an account that has one

## Context and Problem Statement

A user was a repository name, a password and a TOTP second factor, all three
required ([auth](../auth.md)). Registration could not finish without an
authenticator, which is friction for an owner who wants to try a substrate
first and add the factor later. The only way past it was
`SUBSTRATE_INSECURE_DISABLE_TOTP`, which turns the factor off for the whole
deployment and is for local development only. Making the factor optional per
account also raises a second question: how sign-in learns whether to ask for a
code, when the door answers every failure the same way so that nothing about
an account leaks.

## Considered Options

- Keep the factor mandatory for every account.
- An optional factor, with one sign-in form whose code field is always shown
  and optional. Every failure stays one `auth` answer.
- An optional factor, with a two-step sign-in: the password first, and a
  distinct `totp_required` answer only when the password is right and the
  account has a factor.
- An optional factor that an operator setting may forbid.

## Decision Outcome

Chosen: an optional factor with a two-step sign-in, always allowed. A
registration may send `skipTotp: true` in place of an enrollment and a code.
The credential is then written with no `totpRef`, and the password alone signs
in. The owner turns a factor on later through the same two calls that replace
one (`/totp/enroll`, then `/totp`), proving the password alone. A sign-in, a
password change, a re-enrollment or a delete without a code, on an account that
has a factor, answers `401 totp_required` once the password verifies, and the
client then asks for the code.

The two-step answer won over an always-shown optional field because the field
reads as "maybe" to every owner, while the answer asks only the owners who have
a factor. It costs one hint, given only to a caller who proved the password.
A forbidding setting was left out because nothing asked for it yet, and the
discovery document can carry one when something does.

The skip is explicit. An empty seed without `skipTotp` is refused, so a client
that lost its enrollment on the way cannot register an account without a
factor by accident. Under `SUBSTRATE_INSECURE_DISABLE_TOTP`, a registration
that sends no seed also has no factor. It used to get a minted seed that
nobody held, which locked the account out once the flag came off.

### Consequences

- Good, because an owner can register with a name and a password and add the
  factor when they are ready, on any deployment.
- Good, because whether an account has a factor is on its credential record
  (`totpRef` present or absent), so the console reads it like any other record
  and the changelog records the day it was turned on.
- Good, because the door no longer needs the deployment's policy to decide
  whether a credential change owes a code. The account decides.
- Bad, because a right password with no code is now distinguishable from a
  wrong one. An attacker holding a guessed password learns it is right, and
  learns that the account has a factor.
- Bad, because an account without a factor is protected by its password alone,
  and the substrate does not push the owner to turn one on beyond the Settings
  row.
- Bad, because every password client has a second round trip for an account
  with a factor, and a new error code (`totp_required`) to handle. An older
  client shows that answer as a refused sign-in.
- Bad, because there is no way back to no factor, short of the operator's
  `user reset`, which issues one.

### Confirmation

`TestRegistrationMaySkipTheSecondFactor` (engine and API),
`TestLoginWithoutACodeAsksForOne`, the conformance case and the e2e ERR-04 case
for `totp_required`, the console's login, register and account tests, and the
CLI's `TestLoginPromptsForWhatItIsNotGiven` and
`TestRegisterSkipsTheSecondFactorWhenAsked`.

## More Information

Revisit if a deployment needs to require the factor of every account. The
discovery document's `registration` block is where that requirement would go.
