// @vitest-environment jsdom
/** Where an AI model row's key comes from, read once for every surface that
 * asks: an agent's "can it answer yet" and the count beside Providers in the
 * sidebar and on Home. A row with no key of its own still resolves where
 * the operator serves it; the operator's read is asked for only where
 * server.json lists `operatorDefaults`. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

import { useModelKeys, useProvidersInUse } from "./use-model-keys"

const LLM_PROVIDER = "substrate.reamde.dev/llm/provider"
const PROV = "providers.substrate.reamde.dev"

function row(id: string, properties: Record<string, unknown> = {}) {
  return {
    id,
    kind: LLM_PROVIDER,
    version: 1,
    properties: { label: id, wire: "openai", ...properties },
  } as unknown as SubstrateRecord
}

function status(authority: string, pkg: string) {
  return {
    id: `${authority}/${pkg}`,
    name: pkg,
    authority,
    package: pkg,
    installed: true,
    enabled: true,
    accounts: 0,
    functions: 0,
    kinds: 0,
    liveRecords: 0,
  }
}

let features: string[] = []
const asked: string[] = []

beforeEach(() => {
  features = []
  asked.length = 0
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = String(input)
      asked.push(url)
      if (url === "/.well-known/substrate/server.json")
        return Response.json({
          features: features.map((name) => ({ name, stability: "beta" })),
        })
      if (url === "/api/v1/operator/defaults")
        return Response.json({
          operator: { name: "Crane" },
          items: [
            {
              id: "llm/anthropic",
              slot: "llm",
              label: "Anthropic",
              source: "operator",
              operatorOffers: true,
              readiness: "ready",
            },
          ],
        })
      if (url === "/api/v1/substrate.reamde.dev/core/bundle/status")
        return Response.json({
          items: [
            status(PROV, "google"),
            status(PROV, "whoop"),
            status("ada.example.com", "tasks"),
          ],
        })
      if (url.startsWith("/api/v1/records"))
        return Response.json({
          records: [
            row("openai", { apiKey: "••••" }),
            row("anthropic"),
            row("gemini"),
          ],
        })
      return Response.json({})
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Probe() {
  const keys = useModelKeys()
  const inUse = useProvidersInUse()
  return (
    <dl>
      <dt>anthropic</dt>
      <dd data-testid="anthropic">{String(keys.resolves("anthropic"))}</dd>
      <dt>count</dt>
      <dd data-testid="count">{String(inUse.count)}</dd>
      <dt>models</dt>
      <dd data-testid="models">{(inUse.models ?? []).join(", ")}</dd>
    </dl>
  )
}

function renderProbe() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Probe />
    </QueryClientProvider>
  )
}

describe("useModelKeys and useProvidersInUse", () => {
  it("count the provider packages and the AI models whose key resolves, the operator's included", async () => {
    features = ["operatorDefaults"]
    renderProbe()
    await waitFor(() =>
      expect(screen.getByTestId("count").textContent).toBe("4")
    )
    expect(screen.getByTestId("anthropic").textContent).toBe("true")
    expect(screen.getByTestId("models").textContent).toBe("OpenAI, Anthropic")
  })

  it("read a row's own key alone where server.json lists no operatorDefaults", async () => {
    renderProbe()
    await waitFor(() =>
      expect(screen.getByTestId("count").textContent).toBe("3")
    )
    expect(screen.getByTestId("anthropic").textContent).toBe("false")
    expect(asked).not.toContain("/api/v1/operator/defaults")
  })
})
