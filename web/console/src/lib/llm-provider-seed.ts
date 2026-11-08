/** The three llm/provider rows a repository is born with, keyless and
 * unpriced (`internal/engine/seedproviders.go`, `samples/llm/providers.yaml`):
 * the models each offers, and what they cost, are the seeded llm/model
 * records (decision record 0242). Services' Restore writes one back after
 * its owner deleted it: the seed is create-only and never brings a deleted
 * row back on its own. llm-provider-seed.test.ts holds this copy equal to
 * the shipped sample. */

export const LLM_PROVIDER_SEED: Readonly<
  Record<string, Readonly<Record<string, unknown>>>
> = {
  openai: {
    label: "OpenAI",
    wire: "openai",
    baseURL: "https://api.openai.com/v1",
    cache: { key: true },
  },
  anthropic: {
    label: "Anthropic",
    wire: "anthropic",
    baseURL: "https://api.anthropic.com/",
  },
  gemini: {
    label: "Gemini",
    wire: "openai",
    baseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
  },
}
