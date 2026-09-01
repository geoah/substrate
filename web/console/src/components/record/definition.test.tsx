/** The kind Definition view (owner ask: "I go to the llmproviders kind … I
 * don't have a tab to see its definition — I want to see the kind YAML"). It must show
 * the declaration as YAML AND as a readable table — name, type, description,
 * required — and it must do both off the registry it was handed, with no
 * fetch of its own. */

import { cleanup, render, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  }) => (
    <a
      href={Object.entries(params ?? {}).reduce(
        (path, [key, value]) => path.replace(`$${key}`, value),
        to
      )}
      {...rest}
    >
      {children}
    </a>
  ),
}))

import { KindDefinition } from "./definition"
import type { KindInfo } from "@/lib/api/types"

const llmprovider: KindInfo = {
  identity: "core.substrate.reamde.dev/llmprovider",
  name: "llmprovider",
  authority: "core.substrate.reamde.dev",
  version: 1,
  plural: "llmproviders",
  source: "builtin",
  definition: {
    authority: "core.substrate.reamde.dev",
    names: { singular: "llmprovider", plural: "llmproviders" },
    displayTemplate: "{name} ({wire})",
    properties: {
      wire: {
        type: "string",
        required: true,
        description: "the wire protocol this endpoint speaks",
      },
      apiKey: { type: "secret" },
      tags: { type: "string", repeated: true },
      phase: {
        type: "state",
        states: ["draft", "live"],
        initial: "draft",
      },
      billedTo: {
        type: "reference",
        kind: "account",
        mustExist: true,
        onDelete: "cascade",
        description: "who pays for the calls",
      },
    },
  },
}

const account: KindInfo = {
  identity: "core.substrate.reamde.dev/account",
  name: "account",
  authority: "core.substrate.reamde.dev",
  version: 1,
  plural: "accounts",
  source: "builtin",
}

function renderDefinition(kind: KindInfo = llmprovider) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <KindDefinition kind={kind} kinds={[llmprovider, account]} />
    </QueryClientProvider>
  )
}

afterEach(cleanup)

describe("KindDefinition", () => {
  it("shows the declaration as the document that declared it", () => {
    const { container } = renderDefinition()
    const yaml = container.querySelector("pre")!.textContent ?? ""
    expect(yaml).toContain("kind: core.substrate.reamde.dev/kind")
    expect(yaml).toContain("id: core.substrate.reamde.dev/llmprovider")
    expect(yaml).toContain("displayTemplate: ")
    expect(yaml).toContain(
      "description: the wire protocol this endpoint speaks"
    )
  })

  it("tints that YAML with the record manifest's own renderer", async () => {
    const { container } = renderDefinition()
    await waitFor(() => {
      expect(container.querySelector("pre span[style]")).toBeTruthy()
    })
    const colors = [
      ...container.querySelectorAll<HTMLElement>("pre span[style]"),
    ].map((el) => el.style.color)
    expect(colors.every((c) => c.includes("--shiki-"))).toBe(true)
  })

  it("lists every declared property with its type, required and one-liner", () => {
    const { container } = renderDefinition()
    const text = container.textContent ?? ""
    expect(text).toContain("Properties")
    expect(text).toContain("apiKey")
    expect(text).toContain("secret")
    // repeated reads as a list in the declared spelling
    expect(text).toContain("string[]")
    expect(text).toContain("required")
    expect(text).toContain("the wire protocol this endpoint speaks")
  })

  it("carries a state machine's states on the type's hover", () => {
    const { container } = renderDefinition()
    const hoverable = [
      ...container.querySelectorAll("[data-slot=tooltip-trigger]"),
    ].map((el) => el.textContent)
    expect(hoverable).toContain("state")
  })

  it("lists the references, linking each pin to that collection", () => {
    const { container } = renderDefinition()
    const text = container.textContent ?? ""
    expect(text).toContain("References")
    expect(text).toContain("billedTo")
    expect(text).toContain("who pays for the calls")
    const link = [...container.querySelectorAll("a")].find(
      (a) => a.textContent === "→ account"
    )
    expect(link?.getAttribute("href")).toBe(
      "/data/core.substrate.reamde.dev/account"
    )
  })

  it("says what a reference holds a writer to, key by key", () => {
    const { container } = renderDefinition()
    const text = container.textContent ?? ""
    expect(text).toContain("mustExist")
    expect(text).toContain("onDelete: cascade")
  })

  it("says so plainly when the registry stored no declaration", () => {
    const { container } = renderDefinition({
      ...llmprovider,
      definition: undefined,
    })
    expect(container.textContent).toContain("No stored declaration")
    expect(container.querySelector("pre")).toBeNull()
  })
})
