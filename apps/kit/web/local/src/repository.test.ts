import { SubstrateClient } from "@substrate/client"
import { FakeSubstrate, until } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { steadyConnectivity } from "./connectivity"
import { RecordWrites } from "./entry"
import { openRepository } from "./repository"
import { NotForgotten, databaseName } from "./storage"

const kind = "substrate.reamde.dev/template/item"

let fake: FakeSubstrate
let client: SubstrateClient
let n = 0

beforeEach(() => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  client = new SubstrateClient(fake.origin, "tok")
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const open = (repository: string) =>
  openRepository({
    app: "test",
    repository,
    client,
    kinds: [kind],
    connectivity: steadyConnectivity,
  })

async function kept(name: string): Promise<boolean> {
  return (await indexedDB.databases()).some((d) => d.name === name)
}

describe("a repository's link", () => {
  it("closes its storage, and a sign-out after it deletes what it kept", async () => {
    const repository = `repo-${n++}.example.org`
    fake.seed(kind, "a1", { name: "Milk" })
    const link = await open(repository)
    link.start()
    await until(() => link.linked.statusSnapshot().status === "live")
    fake.offline = true
    await link.outbox.write(RecordWrites.create(kind, "b2", { name: "Eggs" }))

    await link.close()
    await expect(link.storage.readOutbox()).rejects.toThrow()
    expect(await kept(databaseName("test", repository))).toBe(true)
    await link.forget()
    expect(await kept(databaseName("test", repository))).toBe(false)
  })

  it("forgets after a remount that closed the first link and opened another", async () => {
    const repository = `repo-${n++}.example.org`
    const first = await open(repository)
    first.start()
    void first.close()
    const second = await open(repository)
    second.start()
    await until(() => second.linked.statusSnapshot().status === "live")
    await second.forget()
    expect(await kept(databaseName("test", repository))).toBe(false)
  })

  it("says so when another tab holds what a sign-out would delete", async () => {
    const repository = `repo-${n++}.example.org`
    const link = await open(repository)
    const name = databaseName("test", repository)
    // Another tab's copy, one that does not step aside.
    const other = await new Promise<IDBDatabase>((resolve, reject) => {
      const req = indexedDB.open(name)
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => reject(req.error ?? new Error("open"))
    })
    const forgetting = link.forget()
    await expect(forgetting).rejects.toBeInstanceOf(NotForgotten)
    await expect(forgetting).rejects.toThrow(/another tab/)
    other.close()
  })
})
