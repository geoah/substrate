import { describe, expect, it } from "vitest"
import {
  compareServerVersions,
  displayServerVersion,
  judgeServer,
  parseServerVersion,
} from "./version"

const v = (text: string) => {
  const parsed = parseServerVersion(text)
  if (!parsed) throw new Error(`not a version: ${text}`)
  return parsed
}

describe("server versions", () => {
  it("reads releases, described builds and prereleases", () => {
    expect(v("v0.113.0")).toEqual({ major: 0, minor: 113, patch: 0 })
    expect(v("v0.113.0-4-gabc1234")).toMatchObject({ commitsAfter: 4 })
    expect(v("v0.113.0-4-gabc1234-dirty")).toMatchObject({ commitsAfter: 4 })
    expect(v("v0.114.0-pre.1")).toMatchObject({ prerelease: "pre.1" })
    expect(parseServerVersion("dev")).toBeUndefined()
  })

  it("orders a prerelease before its release and a later build after", () => {
    const order = [
      "v0.113.1-0.20261031120000-abc",
      "v0.113.1",
      "v0.113.1-2-gabc1234",
      "v0.114.0-pre.1",
      "v0.114.0-pre.2",
      "v0.114.0-rc.1",
      "v0.114.0",
    ]
    for (let i = 1; i < order.length; i++)
      expect(compareServerVersions(v(order[i - 1]), v(order[i]))).toBeLessThan(
        0
      )
  })

  it("says a version as a screen does", () => {
    expect(displayServerVersion("v0.121.0")).toBe("0.121.0")
    expect(displayServerVersion("v0.121.1-0.20261108194438-f87ffd683992")).toBe(
      "a build of 0.121.1"
    )
    expect(displayServerVersion("dev")).toBe("a development build")
  })
})

describe("judgeServer", () => {
  const metadata = {
    issuer: "https://substrate.example.com",
    authorization_endpoint: "https://substrate.example.com/oauth/authorize",
    token_endpoint: "https://substrate.example.com/oauth/token",
    scopes_supported: ["read:*", "write:*"],
    authorization_details_types_supported: ["package"],
  }

  it("lets what a build can do decide for one between releases", () => {
    expect(
      judgeServer(
        { minimumVersion: "0.200.0", scopeVerbs: ["write"] },
        "dev",
        metadata
      )
    ).toEqual({ compatible: true })
  })

  it("holds a release below the minimum to it", () => {
    expect(
      judgeServer({ minimumVersion: "0.120.0" }, "v0.119.3", metadata)
    ).toMatchObject({ compatible: false, needed: "0.120.0", missing: [] })
  })

  it("says a server with no app authorization cannot give apps access", () => {
    expect(judgeServer({}, "v0.90.0", undefined)).toMatchObject({
      compatible: false,
      missing: ["give apps access of their own"],
    })
  })
})
