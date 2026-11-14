---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0316. Agent spend is counted in US dollars

## Context and Problem Statement

Every amount in the LLM layer is in US dollars, but no stored value says so.

- Model prices are `decimal` USD per million tokens (`llm/model.pricing`).
- A thread's cost is `costUSD`, and so is an agent call's result.
- Spend limits are whole cents: an agent's `budgets.spendCentsPerDay`, the
  owner's `llm/agentbudget.spendCentsPerDay`
  ([0314](0314-an-owner-overrides-an-agents-budgets-and-the-operator-sets-each-repositorys-allowance.md)),
  the repository's `substrate.reamde.dev/llm/spendCentsPerDay` setting, and
  the operator's `budget.centsPerDay`.
- The operator defaults file prices its models in USD too.

The repository has a money datatype,
[0140](0140-money-is-an-integer-amount-of-its-currencys-minor-unit.md)'s
`{amount, currency}`, and no LLM field uses it. The question is whether the
limits, or the whole layer, should carry a currency before the stored shapes
freeze at 1.0.

## Considered Options

- Count all agent spend in US dollars, and say so on every amount and every
  surface, with no stored shape changing.
- Make the limits `money` and admit only USD until prices carry a currency.
- Put a currency on the whole chain: prices, costs, limits and the
  operator's allowance.

## Decision Outcome

Chosen: count all agent spend in US dollars, as an estimate of consumption,
whatever currency a vendor invoices in.

- **Prices and costs are estimates.** A price is stored as exact USD digits
  per million tokens, but a cost is float arithmetic over the tokens a
  vendor reported (`priceFloat` in `internal/engine/agentloop.go`). A cost
  is an estimate of what a call consumed, not a copy of an invoice.
- **Limits are whole US cents.** That covers the agent's cap, the owner's
  override of it, the repository's cap, and the operator's allowance, in the
  file and on the wire (`OperatorAllowance.centsPerDay`, `usedCents`,
  `bySlot[].cents`, and `AuthorizationProviderAgent.spendCentsPerDay`).
- **The names keep their shape.** A field named for cents is documented as
  US cents, and the console writes `US$` on every cap, allowance and cost it
  shows.
- **A person whose vendor bills another currency** enters USD-equivalent
  prices on their `llm/model` rows. Their caps then bound that estimate.

A cap means something only in the currency its spend is counted in. Spend is
summed across providers: a root thread's cost carries its sub-agents', which
may run on other rows. So a currency on the limits alone (the second option)
adds a field that can hold one value, and it does not answer what a cap in
one currency does about spend priced in another.

That question is the third option's. It starts at prices and needs either an
exchange-rate source or one cap per currency, with a rule for the spend a
cap does not cover. Excluding that spend silently would make a cap
incomplete. Today no deployment needs it. 0140 stands for the money
datatype, and this record decides only that the LLM layer's amounts do not
use it.

### Consequences

- Good, because no stored shape changes, and every amount in the layer has
  one currency that a reader cannot get wrong.
- Good, because a cap compares with spend summed across providers without a
  conversion.
- Bad, because a person billed in another currency keeps their prices in
  USD by hand, and their caps track that estimate rather than their invoice.
- Bad, because enforcing caps in another currency later is a breaking
  change to the limits, the wire and the operator file. The `llm` package
  and the agent routes are experimental
  ([docs/stability.md](../stability.md)), which is what allows it.

### Confirmation

None: this is held by review only. The console's money formatters
(`capWords` and `costWords` in `web/console/src/lib/agent-runs.ts`) write
`US$`, and their tests hold the text.

## More Information

Revisit when a deployment needs caps enforced in its own currency without
entering USD-equivalent prices by hand. A non-USD invoice alone is not that
need. The step then starts with a currency on `llm/model.pricing` and in the
operator file, and decides what a cap does about spend in another currency.
