import { SubstrateClient, type SubstrateRecord } from "@substrate/client"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { KIND, PROFILE_ID, Profile, type ProfileDraft } from "../kinds"
import {
  changeProfile,
  compareWithTheirs,
  createProperties,
  saveProfile,
  type ChangeDeps,
  type Current,
  type SaveDeps,
} from "./save"

// A substrate behind `fetch` as far as a profile's writes reach it: a PUT
// held to `ifVersion` (a 409 where the row is there), a read of one record.
// @substrate/testing's FakeSubstrate answers the same; this app does not
// depend on it.
class Server {
  readonly records = new Map<string, SubstrateRecord>()
  readonly seen: string[] = []

  seed(properties: Record<string, unknown>): SubstrateRecord {
    const r: SubstrateRecord = {
      id: PROFILE_ID,
      kind: KIND.profile,
      properties,
      version: 3,
      createdAt: "2026-11-01T10:00:00Z",
      updatedAt: "2026-11-01T10:00:00Z",
    }
    this.records.set(PROFILE_ID, r)
    return r
  }

  fetch = async (input: string | URL | Request, init: RequestInit = {}) => {
    const url = new URL(input instanceof Request ? input.url : input.toString())
    const method = init.method ?? "GET"
    this.seen.push(`${method} ${url.pathname}`)
    const id = decodeURIComponent(url.pathname.split("/").pop() ?? "")
    const here = this.records.get(id)
    const json = (status: number, body: unknown) =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      })
    if (method === "GET")
      return here
        ? json(200, here)
        : json(404, { error: { code: "not_found", message: "Not found." } })
    if (method === "PUT") {
      const body = JSON.parse(
        typeof init.body === "string" ? init.body : "{}"
      ) as {
        properties: Record<string, unknown>
        ifVersion?: number
      }
      if (
        body.ifVersion !== undefined &&
        body.ifVersion !== (here?.version ?? 0)
      )
        return json(409, {
          error: { code: "conflict", message: "The record changed." },
        })
      const r: SubstrateRecord = {
        id,
        kind: KIND.profile,
        properties: { ...here?.properties, ...body.properties },
        version: (here?.version ?? 0) + 1,
        createdAt: "2026-11-01T10:00:00Z",
        updatedAt: "2026-11-01T10:00:00Z",
      }
      this.records.set(id, r)
      return json(200, r)
    }
    return json(405, { error: { code: "method", message: "No." } })
  }
}

let server: Server
let client: SubstrateClient
let edits: { draft: Partial<ProfileDraft>; version: number }[]
let changes: Partial<ProfileDraft>[]
let laid: number

beforeEach(() => {
  server = new Server()
  vi.stubGlobal("fetch", server.fetch)
  client = new SubstrateClient("https://substrate.example.com", "tok")
  edits = []
  changes = []
  laid = 0
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function deps(current: Current): SaveDeps & ChangeDeps {
  return {
    current: () => Promise.resolve(current),
    client,
    edit: (draft, version) => {
      edits.push({ draft, version })
      return Promise.resolve({ kept: true })
    },
    change: (draft) => {
      changes.push(draft)
      return Promise.resolve({ kept: true })
    },
    laid: () => {
      laid++
      return Promise.resolve()
    },
  }
}

const absent: Current = { state: "absent" }
const started = "2026-11-09T09:00:00.000Z"

describe("saving a profile none of the owner's devices has", () => {
  it("creates it at ifVersion 0 with its title, the contact and what was filled", async () => {
    const out = await saveProfile(
      deps(absent),
      { birthDate: "1984-05-17", sex: "male", startedAt: started },
      0,
      { contact: "c1" }
    )
    expect(out).toEqual({ state: "saved" })
    expect(server.seen).toEqual([
      "PUT /api/v1/substrate.reamde.dev/healthspan/profile/owner",
    ])
    expect(server.records.get(PROFILE_ID)?.properties).toEqual({
      name: "Healthspan profile",
      contact: `${KIND.contact}/c1`,
      birthDate: "1984-05-17",
      sex: "male",
      startedAt: started,
    })
    expect(laid).toBe(1)
    expect(edits).toEqual([])
  })

  it("is done on a 409 whose row holds the same values, adding what it lacks", async () => {
    server.seed({
      name: "Healthspan profile",
      birthDate: "1984-05-17",
      sex: "male",
    })
    const out = await saveProfile(
      deps(absent),
      { birthDate: "1984-05-17", sex: "male", startedAt: started },
      0,
      { contact: "c1" }
    )
    expect(out).toEqual({ state: "saved" })
    expect(server.seen).toEqual([
      "PUT /api/v1/substrate.reamde.dev/healthspan/profile/owner",
      "GET /api/v1/substrate.reamde.dev/healthspan/profile/owner",
    ])
    // Nothing the other device wrote is sent again; the start and the
    // contact it lacks go as an edit on the version read.
    expect(edits).toEqual([
      { draft: { contact: "c1", startedAt: started }, version: 3 },
    ])
    expect(laid).toBe(1)
  })

  it("shows both on a 409 whose row holds another value", async () => {
    server.seed({
      name: "Healthspan profile",
      birthDate: "1984-05-17",
      sex: "female",
    })
    const out = await saveProfile(
      deps(absent),
      { birthDate: "1984-05-17", sex: "male", startedAt: started },
      0
    )
    expect(out.state).toBe("conflict")
    if (out.state !== "conflict") return
    expect(out.theirs).toMatchObject({
      version: 3,
      birthDate: "1984-05-17",
      sex: "female",
    })
    expect(edits).toEqual([])
    // The owner picks; the pick is an edit on the version they saw.
    const picked = await saveProfile(
      deps({ state: "present", value: out.theirs }),
      { sex: "male" },
      out.theirs.version
    )
    expect(picked).toEqual({ state: "saved" })
    expect(edits).toEqual([{ draft: { sex: "male" }, version: 3 }])
  })

  it("keeps the form's words where the server could not be reached", async () => {
    vi.stubGlobal("fetch", () =>
      Promise.reject(new TypeError("Failed to fetch"))
    )
    const out = await saveProfile(deps(absent), { sex: "male" }, 0)
    expect(out).toEqual({
      state: "failed",
      words: "Couldn't reach the server. Check your connection and try again.",
    })
  })
})

describe("saving a profile that is there", () => {
  const theirs = Profile.read({
    id: PROFILE_ID,
    kind: KIND.profile,
    properties: { name: "Healthspan profile", sex: "female" },
    version: 5,
    createdAt: "2026-11-01T10:00:00Z",
    updatedAt: "2026-11-01T10:00:00Z",
  })

  it("edits on the version the form showed", async () => {
    const out = await saveProfile(
      deps({ state: "present", value: theirs }),
      { sex: "male" },
      4
    )
    expect(out).toEqual({ state: "saved" })
    expect(edits).toEqual([{ draft: { sex: "male" }, version: 4 }])
    expect(server.seen).toEqual([])
  })

  it("compares where the form showed none: another device made it since", async () => {
    const out = await saveProfile(
      deps({ state: "present", value: theirs }),
      { sex: "male" },
      0
    )
    expect(out).toMatchObject({ state: "conflict", theirs: { sex: "female" } })
  })
})

describe("a change no form holds", () => {
  it("creates the profile first where there is none", async () => {
    const words = await changeProfile(
      deps(absent),
      { recordSeenAt: started },
      { contact: "c1" }
    )
    expect(words).toBeUndefined()
    expect(server.records.get(PROFILE_ID)?.properties).toEqual({
      name: "Healthspan profile",
      contact: `${KIND.contact}/c1`,
      recordSeenAt: started,
    })
    expect(changes).toEqual([])
  })

  it("goes on top of one another device made meanwhile", async () => {
    server.seed({ name: "Healthspan profile", sex: "female" })
    const words = await changeProfile(deps(absent), { nightSources: ["a"] })
    expect(words).toBeUndefined()
    expect(changes).toEqual([{ nightSources: ["a"] }])
    expect(laid).toBe(1)
  })
})

describe("comparing", () => {
  it("counts a field the row holds nothing in as added, not different", () => {
    const theirs = Profile.read({
      id: PROFILE_ID,
      kind: KIND.profile,
      properties: { name: "Healthspan profile", sex: "male" },
      version: 1,
      createdAt: "",
      updatedAt: "",
    })
    expect(
      compareWithTheirs({ sex: "male", birthDate: "1990-01-01" }, theirs)
    ).toEqual({ differs: [], adds: { birthDate: "1990-01-01" } })
  })
  it("leaves cleared and empty fields off a new row", () => {
    expect(
      createProperties({ birthDate: null, sex: "male", nightSources: [] })
    ).toEqual({ name: "Healthspan profile", sex: "male" })
  })
})
