/** The shell's crumbs: every page the router serves reads as where it sits,
 * in everyday words by default and as the reference in technical mode, and a
 * parent crumb links back to a route that exists. */

import { describe, expect, it } from "vitest"

import { crumbsFor } from "./app-shell"

describe("crumbsFor", () => {
  it("names the fixed pages", () => {
    expect(crumbsFor("/")).toEqual([{ label: "Home" }])
    expect(crumbsFor("/data")).toEqual([{ label: "All data" }])
    expect(crumbsFor("/history")).toEqual([{ label: "History" }])
    expect(crumbsFor("/changelog")).toEqual([{ label: "History" }])
    expect(crumbsFor("/settings")).toEqual([{ label: "Settings" }])
    expect(crumbsFor("/account/tokens")).toEqual([{ label: "Settings" }])
    expect(crumbsFor("/tools")).toEqual([{ label: "Tools" }])
    expect(crumbsFor("/providers")).toEqual([{ label: "Providers" }])
    expect(crumbsFor("/agents")).toEqual([{ label: "Agents" }])
  })

  it("reads a record in everyday words: where it lives, its collection, the record", () => {
    expect(crumbsFor("/data/a.example.com/tasks/task/t1")).toEqual([
      { label: "Your data", to: "/data" },
      {
        label: "Tasks",
        kind: "a.example.com/tasks/task",
        to: "/data/a.example.com/tasks/task",
      },
      {
        label: "Untitled task",
        record: { kind: "a.example.com/tasks/task", id: "t1" },
      },
    ])
  })

  it("reads a provider's collection as coming from it", () => {
    const [from, contacts] = crumbsFor(
      "/data/providers.substrate.reamde.dev/google/contact"
    )
    expect(from).toEqual({
      label: "From Google",
      to: "/data",
      provider: "google",
    })
    expect(contacts).toMatchObject({ label: "Contacts" })
    expect(contacts.to).toBeUndefined()
  })

  it("spells the reference segment by segment in technical mode", () => {
    expect(crumbsFor("/data/a.example.com/tasks/task/t1", true)).toEqual([
      { label: "a.example.com", to: "/data/a.example.com", mono: true },
      { label: "tasks", to: "/data/a.example.com/tasks", mono: true },
      {
        label: "task",
        kind: "a.example.com/tasks/task",
        mono: true,
        to: "/data/a.example.com/tasks/task",
      },
      {
        label: "t1",
        mono: true,
        record: { kind: "a.example.com/tasks/task", id: "t1" },
      },
    ])
  })

  it("links the record back from its edit page", () => {
    const crumbs = crumbsFor("/data/a.example.com/tasks/task/t1/edit")
    expect(crumbs.at(-2)?.to).toBe("/data/a.example.com/tasks/task/t1")
    expect(crumbs.at(-1)).toEqual({ label: "Edit" })
    expect(crumbsFor("/data/a.example.com/tasks/task/new").at(-1)).toEqual({
      label: "New task",
    })
  })

  it("reads a provider and a tool under their pages", () => {
    expect(
      crumbsFor("/providers/providers.substrate.reamde.dev/google")
    ).toEqual([
      { label: "Providers", to: "/providers" },
      { label: "Google", provider: "google" },
    ])
    expect(
      crumbsFor("/providers/providers.substrate.reamde.dev/google", true)
    ).toEqual([
      { label: "Providers", to: "/providers" },
      { label: "providers.substrate.reamde.dev/google", mono: true },
    ])
    expect(crumbsFor("/tools/a.example.com/notes/stats")).toEqual([
      { label: "Tools", to: "/tools" },
      { label: "Stats" },
    ])
    expect(crumbsFor("/tools/a.example.com/notes/savenote")).toEqual([
      { label: "Tools", to: "/tools" },
      { label: "Save note" },
    ])
    expect(
      crumbsFor("/tools/providers.substrate.reamde.dev/google/synccontacts")
    ).toEqual([
      { label: "Tools", to: "/tools" },
      { label: "Google Contacts sync" },
    ])
  })

  it("reads an actor under History, by name unless technical", () => {
    expect(crumbsFor("/actors/console")).toEqual([
      { label: "History", to: "/history" },
      { label: "You" },
    ])
    expect(crumbsFor("/actors/console", true)).toEqual([
      { label: "History", to: "/history" },
      { label: "console", mono: true },
    ])
  })

  it("keeps an authority and a package page under All data, in words", () => {
    expect(crumbsFor("/data/a.example.com")).toEqual([
      { label: "All data", to: "/data" },
      { label: "Your data" },
    ])
    expect(crumbsFor("/data/a.example.com/tasks")).toEqual([
      { label: "All data", to: "/data" },
      { label: "Your data", to: "/data/a.example.com" },
      { label: "Tasks package" },
    ])
    expect(crumbsFor("/data/providers.substrate.reamde.dev/google")).toEqual([
      { label: "All data", to: "/data" },
      {
        label: "From providers",
        to: "/data/providers.substrate.reamde.dev",
      },
      { label: "Google", provider: "google" },
    ])
  })

  it("spells an authority and a package as themselves in technical mode", () => {
    expect(crumbsFor("/data/a.example.com/tasks", true)).toEqual([
      { label: "All data", to: "/data" },
      { label: "a.example.com", to: "/data/a.example.com", mono: true },
      { label: "tasks", mono: true },
    ])
  })
})
