/** The bundle surface: the lifecycle state derivation the list + detail read
 * from a status, and the verbs POST to the computed lifecycle endpoints under
 * the bundle's owned-authority id. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  ACCOUNT_CONFIG_TRAIT,
  bindBundleInput,
  bundleState,
  parseSubstrateOAuthMessage,
  purgeBundle,
  runBundleVerb,
  setupCount,
  startOAuth,
  SUBSTRATE_OAUTH_SOURCE,
  traitRecordsQueryOptions,
  type BundleStatus,
} from "./bundles"

function status(over: Partial<BundleStatus> = {}): BundleStatus {
  return {
    id: "samples.substrate.reamde.dev/web",
    name: "web",
    authority: "samples.substrate.reamde.dev",
    package: "web",
    installed: true,
    enabled: true,
    inputs: [
      {
        name: "connector",
        kind: "samples.substrate.reamde.dev/web/config",
        record: "default",
        via: "default",
      },
    ],
    accounts: 0,
    functions: 1,
    kinds: 2,
    liveRecords: 3,
    ...over,
  }
}

describe("bundleState", () => {
  it("reads uninstalled first — the marker overrides everything", () => {
    expect(bundleState(status({ installed: false, enabled: false }))).toBe(
      "uninstalled"
    )
  })
  it("reads disabled when installed but execution stopped", () => {
    expect(bundleState(status({ enabled: false }))).toBe("disabled")
  })
  it("reads enabled when installed and running; setup never moves it", () => {
    expect(bundleState(status())).toBe("enabled")
    expect(
      bundleState(
        status({
          setup: [
            {
              code: "missing",
              input: "connector",
              kind: "samples.substrate.reamde.dev/web/config",
              message: "no config record exists yet",
            },
          ],
        })
      )
    ).toBe("enabled")
  })
})

describe("setupCount", () => {
  it("is zero for a bundle with no setup key (ready is the absent list)", () => {
    expect(setupCount(status())).toBe(0)
    expect(setupCount(status({ setup: [] }))).toBe(0)
  })
  it("counts the standing steps", () => {
    expect(
      setupCount(
        status({
          setup: [
            { code: "missing", input: "connector", message: "m" },
            { code: "provider", record: "openai", message: "p" },
          ],
        })
      )
    ).toBe(2)
  })
})

describe("lifecycle verbs", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => vi.stubGlobal("fetch", fetchMock))
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("PATCHes the bundle record's disabled state by its owned-authority id", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(status({ enabled: false })), { status: 200 })
    )
    await runBundleVerb("samples.substrate.reamde.dev/web", "disable")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/substrate.reamde.dev/core/bundle/samples.substrate.reamde.dev%2Fweb"
    )
    expect(init?.method).toBe("PATCH")
    expect(JSON.parse(String(init?.body))).toEqual({
      properties: { disabled: true },
    })
  })

  it("purge PATCHes the purging state and returns the tombstoned-row count", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ purged: 12 }), { status: 200 })
    )
    const res = await purgeBundle("samples.substrate.reamde.dev/web")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/substrate.reamde.dev/core/bundle/samples.substrate.reamde.dev%2Fweb"
    )
    expect(init?.method).toBe("PATCH")
    expect(JSON.parse(String(init?.body))).toEqual({
      properties: { purging: true },
    })
    expect(res.purged).toBe(12)
  })

  it("bind POSTs the input name and record to the bundle's bind sub-path", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(status()), { status: 200 })
    )
    const res = await bindBundleInput(
      "samples.substrate.reamde.dev/web",
      "connector",
      "rec-1"
    )
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/substrate.reamde.dev/core/bundle/samples.substrate.reamde.dev%2Fweb/bind"
    )
    expect(init?.method).toBe("POST")
    expect(JSON.parse(String(init?.body))).toEqual({
      input: "connector",
      record: "rec-1",
    })
    expect(res.id).toBe("samples.substrate.reamde.dev/web")
  })

  it("bind with an empty record is the unbind", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(status()), { status: 200 })
    )
    await bindBundleInput("samples.substrate.reamde.dev/web", "connector", "")
    const [, init] = fetchMock.mock.calls[0]
    expect(JSON.parse(String(init?.body))).toEqual({
      input: "connector",
      record: "",
    })
  })

  it("oauth/start sends the account record id and returns the consent url", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ url: "https://consent" }), { status: 200 })
    )
    const res = await startOAuth("acct-1")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe("/api/v1/oauth/start")
    expect(JSON.parse(String(init?.body))).toEqual({ record: "acct-1" })
    expect(res.url).toBe("https://consent")
  })
})

describe("the account-config trait read", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => vi.stubGlobal("fetch", fetchMock))
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  // The trait is a KIND REFERENCE, and the Accounts section reads it through
  // the core package's trait sub-path. A spelling the server does not
  // recognize is a 404 nobody sees until the section is opened, so the
  // identity and the URL it produces are pinned here together.
  it("reads the host trait by its identity, under the core package", async () => {
    expect(ACCOUNT_CONFIG_TRAIT).toBe("substrate.reamde.dev/core/accountconfig")
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ records: [] }), { status: 200 })
    )
    await traitRecordsQueryOptions(ACCOUNT_CONFIG_TRAIT).queryFn?.({
      signal: undefined,
    } as never)
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      "/api/v1/substrate.reamde.dev/core/trait/substrate.reamde.dev%2Fcore%2Faccountconfig/records?first=200"
    )
  })
})

describe("parseSubstrateOAuthMessage — the callback return contract", () => {
  it("parses a success into the connected record id", () => {
    expect(
      parseSubstrateOAuthMessage({
        source: SUBSTRATE_OAUTH_SOURCE,
        ok: true,
        record: "acct-1",
      })
    ).toEqual({ ok: true, record: "acct-1" })
  })

  it("parses a failure into its correlation id", () => {
    expect(
      parseSubstrateOAuthMessage({
        source: SUBSTRATE_OAUTH_SOURCE,
        ok: false,
        correlation: "corr-9",
      })
    ).toEqual({ ok: false, correlation: "corr-9" })
  })

  it("keeps a failure even when the correlation id is missing (empty string)", () => {
    expect(
      parseSubstrateOAuthMessage({ source: SUBSTRATE_OAUTH_SOURCE, ok: false })
    ).toEqual({ ok: false, correlation: "" })
  })

  it("ignores a message whose source is not substrate-oauth", () => {
    expect(
      parseSubstrateOAuthMessage({
        source: "other",
        ok: true,
        record: "acct-1",
      })
    ).toBeNull()
    expect(
      parseSubstrateOAuthMessage({ ok: true, record: "acct-1" })
    ).toBeNull()
  })

  it("ignores a success with no record id (a connected row must be named)", () => {
    expect(
      parseSubstrateOAuthMessage({ source: SUBSTRATE_OAUTH_SOURCE, ok: true })
    ).toBeNull()
    expect(
      parseSubstrateOAuthMessage({
        source: SUBSTRATE_OAUTH_SOURCE,
        ok: true,
        record: "",
      })
    ).toBeNull()
  })

  it("ignores non-objects and a missing or non-boolean ok", () => {
    expect(parseSubstrateOAuthMessage(null)).toBeNull()
    expect(parseSubstrateOAuthMessage("substrate-oauth")).toBeNull()
    expect(parseSubstrateOAuthMessage(42)).toBeNull()
    expect(
      parseSubstrateOAuthMessage({ source: SUBSTRATE_OAUTH_SOURCE, ok: "yes" })
    ).toBeNull()
    expect(
      parseSubstrateOAuthMessage({ source: SUBSTRATE_OAUTH_SOURCE })
    ).toBeNull()
  })
})
