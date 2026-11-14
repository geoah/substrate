# Approvals: standing approvals the owner grants

Status: proposal, 2026-11-09. What exists is the hold itself
([0254](../decisions/0254-an-agents-call-of-a-function-that-declares-approval-waits-for-the-owner-in-chat.md)):
an agent's call of a function declaring `approval: required` waits for the
owner in chat. Nothing here is built.

## The problem

Every held call waits for a person. That is right for Moss in a chat, where
the owner is reading. It is wrong for work the owner set up to run alone: a
trigger whose agent files the weekly report by mail, or a morning agent that
accepts invitations from the owner's own team. Today those calls wait in a
thread nobody opens, and nothing expires them.

The owner wants some agents, triggers and, later, workflows to run such a
call without asking, because they were set up that way. They also want an
agent to be able to skip the wait when it is certain. Neither may become a
way for a model, a bundle or a prompt injected through the mail it reads to
grant itself a send.

## The rules any design must keep

1. **Only the owner grants.** A standing approval is written by the owner's
   hand: the console, or their own token. A bundle-tier actor, an agent, a
   function and an app's scoped token are refused, as they are for
   `recordpatchpolicy` today. A package that ships an agent ships no
   standing approval for it.
2. **A grant names what it answers.** Exact function references and exactly
   one holder, as an allow outranks a gate only by naming it
   ([0108](../decisions/0108-an-allow-outranks-a-gate-only-by-naming-it.md)).
   No globs in the first version: `google/*` would also cover the next
   function Google ships.
3. **Certainty narrows, never widens.** An agent's claim that it is sure is
   a condition inside a grant, never a grant. Without a standing approval
   for the holder and the function, nothing the model says skips the wait.
4. **Every call leaves its record.** A call that skips the wait still lands
   an `llm/approval`, born decided, citing the grant that decided it, and
   runs through the same path. The chat shows it as run under a standing
   approval, so what happened without the owner is as visible as what
   happened with them.
5. **Revoking is one write.** Deleting or disabling the grant restores the
   wait for the next call. A call already running is not stopped.

## The shape

A new owner-only kind, `substrate.reamde.dev/llm/standingapproval`:

```yaml
kind: substrate.reamde.dev/llm/standingapproval
metadata:
  id: weekly-report-mail
data:
  properties:
    holder:
      ref: substrate.reamde.dev/core/trigger/weekly-report
    functions:
      - ref: substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/sendmail
    when: always          # or: certain
    condition: >-
      args.to.all(a, a.endsWith("@example.com"))
    maxPerDay: 3
    expiresAt: 2027-03-01T00:00:00Z
    disabled: false
```

- **`holder`** is one agent, one trigger, or (once they exist) one workflow.
  An agent holder covers its calls in any thread. A trigger holder covers
  only the agent runs that trigger delivered, so the same agent in chat
  still asks.
- **`when: always`** skips the wait for every covered call. **`when:
  certain`** adds a boolean argument to the covered tools' cards, which the
  model sets only when it is sure. The call skips the wait only with it set,
  and asks otherwise. The argument appears only where such a grant exists,
  so no other agent's card changes.
- **`condition`** is an optional CEL expression over the call's arguments.
  A call it rejects asks the owner as usual. A send limited to the owner's
  own domain is the case that motivates it.
- **`maxPerDay`** and **`expiresAt`** bound a grant that turns out wrong. A
  call past either asks the owner.

The loop's dispatch reads the grants at call time, after it resolves the
tool and before it lands the approval. A grant that applies lands the
approval as `approved` with `grant` naming it, and the run proceeds as an
owner's approval does today.

## Creating one "that way"

The owner sets the wait up where they set up the work:

- **An agent's page** in the console lists the functions its tools would
  hold, each with "Ask me every time" (the default), "Only when it's sure"
  and "Never ask", writing or removing the grant.
- **A trigger's set-up** offers the same choice for the agent it delivers
  to, written with the trigger as the holder.
- **The approval card** offers "Always allow this for <agent>" beside
  Approve. It writes the grant only after a second confirmation that names
  what it covers.

## Pending approvals nobody sees

Independent of grants, a pending approval in a thread with no owner reading
it needs a way out:

- notify, as questions and suggestions do today (the chat app already
  counts and notifies them);
- an expiry, after which the approval is declined as `expired` and reported,
  so the agent's thread can end;
- a console list of everything waiting, across threads.

## Order of work

1. The pending list and the expiry. These are useful without grants.
2. The `standingapproval` kind, `when: always`, exact functions, an agent
   or trigger holder, and the console's controls. Then a decision record.
3. `condition` and the limits.
4. `when: certain`, once there is evidence that models set the flag
   honestly enough to be worth having. Until then, a narrower `condition`
   is the safer tool.

## Open questions

- Whether an agent holder should also cover the sub-agents it calls. The
  answer proposed here is no: each agent is granted by name.
- Whether `condition` sees the resolved account (which address sends), not
  only the arguments.
- Whether a declined call should teach the agent anything beyond the
  `approvalDecided` row it reads today.
