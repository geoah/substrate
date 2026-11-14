// @vitest-environment jsdom
/** The groups the sidebar and Home list: a provider's once its four set-up
 * steps are done, the same steps its card on Providers reads, or while any
 * of its collections holds a record; every other group always. A set-up
 * started and left adds nothing, and its collections are not counted when
 * the package holds no row at all. */

import { useMemo } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  ACCOUNT_CONFIG_TRAIT,
  bundleStatusesQueryOptions,
  traitRecordsQueryOptions,
} from "@/lib/api/bundles"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { BundleStatus, KindInfo, SubstrateRecord } from "@/lib/api/types"
import { collectionGroups } from "@/lib/collections"

import { useListedGroups } from "./use-listed-groups"

const GITHUB = "providers.substrate.reamde.dev/github"

function kind(
  identity: string,
  definition: Record<string, unknown> = {}
): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "published",
    description: "",
    definition: { properties: {}, ...definition },
  }
}

const KINDS = [
  kind("~/tasks/task"),
  kind(`${GITHUB}/config`, {
    purpose: "internal",
    traits: ["substrate.reamde.dev/core/oauth2"],
  }),
  kind(`${GITHUB}/account`, {
    purpose: "internal",
    traits: [
      "substrate.reamde.dev/core/accountconfig",
      "substrate.reamde.dev/core/sync",
    ],
  }),
  kind(`${GITHUB}/issue`),
  kind(`${GITHUB}/pullrequest`),
]

/** GitHub added, and its sign-in details never saved: step 2 of 4. */
function unfinished(over: Partial<BundleStatus> = {}): BundleStatus {
  return {
    id: GITHUB,
    name: "github",
    authority: "providers.substrate.reamde.dev",
    package: "github",
    installed: true,
    enabled: true,
    inputs: [{ name: "client", kind: `${GITHUB}/config` }],
    setup: [
      {
        code: "missing",
        input: "client",
        kind: `${GITHUB}/config`,
        message: "Save GitHub's sign-in details.",
      },
    ],
    accounts: 0,
    functions: 2,
    kinds: 4,
    liveRecords: 0,
    provider: true,
    ...over,
  }
}

function connected(over: Partial<BundleStatus> = {}): BundleStatus {
  return unfinished({
    inputs: [{ name: "client", kind: `${GITHUB}/config`, record: "default" }],
    setup: undefined,
    accounts: 1,
    liveRecords: 2,
    ...over,
  })
}

function account(tokenStatus: string): SubstrateRecord {
  return {
    id: "ada",
    kind: `${GITHUB}/account`,
    properties: { login: "ada", tokenStatus },
    labels: {},
    version: 1,
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
  }
}

let counts: Record<string, number> = {}
let accountsFail = false
const asked: string[] = []

beforeEach(() => {
  counts = {}
  accountsFail = false
  asked.length = 0
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = new URL(String(input), "http://localhost")
      asked.push(url.pathname + url.search)
      const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
        kinds?: string[]
      }
      if (url.searchParams.get("count") === "1" && filter.kinds?.length) {
        return Response.json({
          records: [],
          count: counts[filter.kinds[0]] ?? 0,
        })
      }
      if (accountsFail)
        return Response.json({ code: "internal" }, { status: 500 })
      return Response.json({ code: "not_found" }, { status: 404 })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Listed({ kinds }: { kinds: KindInfo[] }) {
  const groups = useMemo(() => collectionGroups(kinds), [kinds])
  const listed = useListedGroups(groups)
  return (
    <p data-testid="listed">{(listed ?? []).map((g) => g.label).join(", ")}</p>
  )
}

function renderListed(
  statuses: BundleStatus[],
  accounts: SubstrateRecord[] | undefined,
  {
    kinds = KINDS,
    capped = false,
  }: { kinds?: KindInfo[]; capped?: boolean } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(kindsQueryOptions.queryKey, kinds)
  client.setQueryData(bundleStatusesQueryOptions.queryKey, statuses)
  if (accounts)
    client.setQueryData(
      traitRecordsQueryOptions(ACCOUNT_CONFIG_TRAIT).queryKey,
      { records: accounts, capped }
    )
  render(
    <QueryClientProvider client={client}>
      <Listed kinds={kinds} />
    </QueryClientProvider>
  )
  return client
}

async function listed(text: string) {
  expect((await screen.findByText(text)).textContent).toBe(text)
}

/** Every count asked has answered and rendered: a group still left out now
 * stays out. */
async function settled(client: QueryClient, asks: number) {
  await waitFor(() => expect(countsAsked()).toHaveLength(asks))
  await waitFor(() => expect(client.isFetching()).toBe(0))
  await act(async () => {})
}

function countsAsked(): string[] {
  return asked.filter((u) => u.includes("count=1"))
}

describe("useListedGroups", () => {
  it("leaves out a provider added and left at its sign-in details, without counting", async () => {
    renderListed([unfinished()], [])
    await listed("Tasks")
    expect(countsAsked()).toEqual([])
  })

  it("lists a connected provider whose collections are empty", async () => {
    renderListed([connected()], [account("connected")])
    await listed("Tasks, From GitHub")
    expect(countsAsked()).toEqual([])
  })

  it("leaves out a provider whose only account is waiting for approval", async () => {
    const client = renderListed([connected()], [account("pending")])
    await settled(client, 2)
    await listed("Tasks")
  })

  it("lists a provider that is not connected while a collection holds records", async () => {
    counts = { [`${GITHUB}/issue`]: 40 }
    renderListed([unfinished({ liveRecords: 41 })], [])
    await listed("Tasks, From GitHub")
  })

  it("leaves it out once its collections count empty, whatever else its package holds", async () => {
    const client = renderListed([unfinished({ liveRecords: 1 })], [])
    await settled(client, 2)
    await listed("Tasks")
  })

  it("leaves out a provider published outside the providers authority, left at its sign-in details", async () => {
    const acme = "connector.example.com/acme"
    const kinds = [
      ...KINDS,
      kind(`${acme}/config`, {
        purpose: "internal",
        traits: ["substrate.reamde.dev/core/oauth2"],
      }),
      kind(`${acme}/account`, {
        purpose: "internal",
        traits: ["substrate.reamde.dev/core/accountconfig"],
      }),
      kind(`${acme}/ticket`),
    ]
    renderListed(
      [
        connected(),
        unfinished({
          id: acme,
          name: "acme",
          authority: "connector.example.com",
          package: "acme",
          inputs: [{ name: "client", kind: `${acme}/config` }],
        }),
      ],
      [account("connected")],
      { kinds }
    )
    await listed("Tasks, From GitHub")
  })

  it("lists every provider where the accounts read was capped", async () => {
    renderListed([connected()], [account("pending")], { capped: true })
    await listed("Tasks, From GitHub")
  })

  it("reads no accounts where no kind brings one, as on a new repository", async () => {
    const kinds = KINDS.filter((k) => k.identity !== `${GITHUB}/account`)
    const client = renderListed([unfinished()], undefined, { kinds })
    await listed("Tasks")
    await waitFor(() => expect(client.isFetching()).toBe(0))
    expect(asked.filter((u) => u.includes("implements"))).toEqual([])
  })

  it("lists every group where the accounts could not be read", async () => {
    accountsFail = true
    renderListed([unfinished()], undefined)
    await listed("Tasks, From GitHub")
  })
})
