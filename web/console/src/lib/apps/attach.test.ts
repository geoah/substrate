import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  homeViews,
  launcherEntries,
  recordPageParams,
  replacingView,
  viewsAttachedTo,
  viewsForRecord,
} from "./attach"

const TASK = "ada.example.com/tasks/task"
const PROJECT = "ada.example.com/tasks/project"
const PERSON = "ada.example.com/people/person"

function kindInfo(
  identity: string,
  properties: Record<string, unknown>
): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: {
      authority,
      package: pkg,
      names: { singular: name },
      displayTemplate: "{name}",
      properties,
    },
  }
}

const kinds: KindInfo[] = [
  kindInfo(TASK, {
    name: { type: "string", required: true },
    status: { type: "state", states: ["open", "done"], initial: "open" },
    // The pin is the bare singular, completed inside the package.
    project: { type: "reference", kind: "project" },
  }),
  kindInfo(PROJECT, {
    name: { type: "string", required: true },
  }),
  kindInfo(PERSON, {
    name: { type: "string", required: true },
  }),
]

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-12T00:00:00Z",
    updatedAt: "2026-09-12T00:00:00Z",
  }
}

const VIEW = "substrate.reamde.dev/core/view"
const kindRef = (identity: string) => ({
  ref: `substrate.reamde.dev/core/kind/${identity}`,
})

function view(id: string, properties: Record<string, unknown>) {
  return record(VIEW, id, { layout: "list", name: id, ...properties })
}

const views = [
  view("tasks-open", {
    name: "Open tasks",
    kind: kindRef(TASK),
    attach: ["launcher", "browse"],
  }),
  view("tasks-by-project", {
    name: "Open tasks",
    kind: kindRef(TASK),
    via: "project",
    attach: ["record"],
  }),
  view("people-contacts", {
    name: "Contacts",
    layout: "contacts",
    kind: kindRef(PERSON),
    attach: ["launcher", "browse"],
  }),
  view("timeline", {
    name: "Timeline",
    layout: "timeline",
    trait: {
      ref: "substrate.reamde.dev/core/trait/substrate.reamde.dev/core/temporal",
    },
    attach: ["launcher", "home"],
  }),
  view("tasks-table", {
    name: "Every task",
    kind: kindRef(TASK),
    replaces: true,
  }),
  view("tasks-all", {
    name: "All tasks",
    kind: kindRef(TASK),
    replaces: true,
  }),
  view("project-notes", {
    name: "Notes",
    layout: "detail",
    kind: kindRef(PROJECT),
    attach: ["record"],
  }),
]

const apps = [
  record("substrate.reamde.dev/core/app", "github-prs", {
    name: "Pull requests",
    icon: "git-pull-request",
    screens: [{ name: "mine", view: { ref: `${VIEW}/tasks-open` } }],
  }),
  record("substrate.reamde.dev/core/app", "agenda", {
    name: "Agenda",
    screens: [{ name: "today", view: { ref: `${VIEW}/timeline` } }],
  }),
]

const ids = (found: { spec: { id: string } }[]) => found.map((v) => v.spec.id)

describe("viewsAttachedTo", () => {
  it("lists the browse views of one kind and no other", () => {
    expect(ids(viewsAttachedTo(views, kinds, { browse: TASK }))).toEqual([
      "tasks-open",
    ])
    expect(ids(viewsAttachedTo(views, kinds, { browse: PERSON }))).toEqual([
      "people-contacts",
    ])
    expect(viewsAttachedTo(views, kinds, { browse: PROJECT })).toEqual([])
  })
})

describe("viewsForRecord", () => {
  const website = record(PROJECT, "website", { name: "Website" })
  const task = record(TASK, "t1", { name: "Ship it" })

  it("reaches a project through a via pinned at its kind, and through its own kind", () => {
    expect(ids(viewsForRecord(views, kinds, website))).toEqual([
      "project-notes",
      "tasks-by-project",
    ])
  })

  it("does not attach the via view to a record of the view's own kind", () => {
    expect(viewsForRecord(views, kinds, task)).toEqual([])
  })

  it("drops a via the registry cannot resolve and keeps the own-kind view", () => {
    const bare = kinds.filter((k) => k.identity !== PROJECT)
    expect(ids(viewsForRecord(views, bare, website))).toEqual(["project-notes"])
  })
})

describe("homeViews", () => {
  it("lists the views attached to home", () => {
    expect(ids(homeViews(views, kinds))).toEqual(["timeline"])
  })
})

describe("replacingView", () => {
  it("picks the lowest id among the views that replace the table", () => {
    expect(replacingView(views, kinds, TASK)?.spec.id).toBe("tasks-all")
    expect(replacingView(views, kinds, PERSON)).toBeUndefined()
  })
})

describe("launcherEntries", () => {
  it("lists launcher views then apps, each with an icon and a route", () => {
    expect(launcherEntries(views, apps, kinds)).toEqual([
      {
        key: "view:people-contacts",
        label: "Contacts",
        icon: "contact",
        to: "/views/people-contacts",
      },
      {
        key: "view:tasks-open",
        label: "Open tasks",
        icon: "list",
        to: "/views/tasks-open",
      },
      {
        key: "view:timeline",
        label: "Timeline",
        icon: "calendar-range",
        to: "/views/timeline",
      },
      {
        key: "app:agenda",
        label: "Agenda",
        icon: "layout-grid",
        to: "/apps/agenda",
      },
      {
        key: "app:github-prs",
        label: "Pull requests",
        icon: "git-pull-request",
        to: "/apps/github-prs",
      },
    ])
  })

  it("is empty without launcher views or apps", () => {
    expect(launcherEntries([], [], kinds)).toEqual([])
  })
})

describe("recordPageParams", () => {
  it("splits the record's kind into the data route's segments", () => {
    expect(recordPageParams(record(TASK, "t1", {}))).toEqual({
      authority: "ada.example.com",
      pkg: "tasks",
      name: "task",
      id: "t1",
    })
  })
})
