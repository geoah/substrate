/** The three llm/provider rows a repository is born with, keyless
 * (`internal/engine/seedproviders.go`, `samples/llm/providers.yaml`).
 * Services' Restore writes one back after its owner deleted it: the seed is
 * create-only and never brings a deleted row back on its own.
 * llm-provider-seed.test.ts holds this copy equal to the shipped sample. */

export const LLM_PROVIDER_SEED: Readonly<
  Record<string, Readonly<Record<string, unknown>>>
> = {
  openai: {
    label: "OpenAI",
    wire: "openai",
    baseURL: "https://api.openai.com/v1",
    cache: { key: true },
    pricing: [
      {
        model: "gpt-5",
        inputPer1M: "1.25",
        outputPer1M: "10",
        contextWindow: 400000,
      },
      {
        model: "gpt-5-mini",
        inputPer1M: "0.25",
        outputPer1M: "2",
        contextWindow: 400000,
      },
    ],
  },
  anthropic: {
    label: "Anthropic",
    wire: "anthropic",
    baseURL: "https://api.anthropic.com/",
    pricing: [
      {
        model: "claude-opus-5-5",
        inputPer1M: "4",
        outputPer1M: "20",
        cacheReadPer1M: "0.20",
        contextWindow: 200000,
      },
      {
        model: "claude-opus-5",
        inputPer1M: "5",
        outputPer1M: "25",
        contextWindow: 200000,
      },
      {
        model: "claude-sonnet-5-5",
        inputPer1M: "2",
        outputPer1M: "10",
        cacheReadPer1M: "0.20",
        contextWindow: 200000,
      },
      {
        model: "claude-sonnet-5",
        inputPer1M: "2",
        outputPer1M: "10",
        cacheReadPer1M: "0.20",
        contextWindow: 200000,
      },
      {
        model: "claude-haiku-5-5",
        inputPer1M: "0.10",
        outputPer1M: "0.50",
        cacheReadPer1M: "0.01",
        contextWindow: 100000,
      },
      {
        model: "claude-haiku-4-5",
        inputPer1M: "1",
        outputPer1M: "5",
        cacheReadPer1M: "0.10",
        contextWindow: 200000,
      },
    ],
  },
  gemini: {
    label: "Gemini",
    wire: "openai",
    baseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
    pricing: [
      {
        model: "gemini-2.5-pro",
        inputPer1M: "1.25",
        outputPer1M: "10",
        contextWindow: 1048576,
      },
      {
        model: "gemini-2.5-flash",
        inputPer1M: "0.15",
        outputPer1M: "0.60",
        contextWindow: 1048576,
      },
    ],
  },
}
