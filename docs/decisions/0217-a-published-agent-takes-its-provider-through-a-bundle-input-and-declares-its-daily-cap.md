---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0217. A published agent takes its provider through a bundle input and declares its daily cap

## Context and Problem Statement

A published agent cannot know the id of its owner's AI provider record.
The owner must choose where its completions are bought before approving
the package. Its publisher must also state the daily spending limit.

## Considered Options

- Let each app choose and write a provider id into its agent.
- Reuse bundle inputs, with an ordered list of supported models.

## Decision Outcome

Use a bundle input. An agent names exactly one of provider with model,
or providerInput with models. The latter names an llm/provider input of
its own bundle and one to eight model ids, best first.

Dispatch resolves the bound record, then default, then the sole record,
using the existing input resolver. The first preferred model priced by
that row wins. An exact id wins over a gateway prefix; otherwise prefixed
matches are ordered lexically. A dated suffix is not a match. A refusal
names the provider row and supported models. Compaction, titles and
accounting use the resolved provider and model.

The approval offers rows that support every agent sharing the input,
showing each resolved model and daily cap. A sole option is preselected;
a still-suitable existing binding is retained. No option blocks with
no-model and links to the AI provider collection. Choices are validated
against the fresh plan at approval and again before setup, then bound
through the owner's dataset after installation.

This amends [0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md):
published agents must declare budgets.spendCentsPerDay between 0 and
1000 inclusive. Owner declarations retain the optional cap. Existing
spend admission semantics remain. It extends the input use described by
[0076](0076-a-bundle-ships-its-settings-as-core-setting-and-secret-records.md)
to an agent's provider; settings and secrets remain ordinary records.

## Consequences

- Provider credentials remain the owner's data, outside published closures.
- One shared input requires a provider compatible with all its agents.
- Model-specific params are not expressible in a single declaration.
  A portable agent can omit params and use provider defaults.
- Zero prices remain valid prices; a free model is still accounted for.
- Revoking the requesting app does not uninstall its published package.

## Confirmation

TestAgentProviderShapes, TestPreferredAgentModel and
TestPublishedAgentBudgets cover parsing and selection.
TestAgentProviderInputResolution covers bound, default and sole resolution,
the refusal and a binding surviving an upgrade.
TestPublishedAgentRequiresDailyCap covers published admission.
TestProviderOptionsAndChoices covers fitting rows and approval validation.
The provider-picker console test covers the model, daily cap and choice.
