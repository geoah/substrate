// @vitest-environment jsdom
/** The contacts layout against a stubbed substrate: the A–Z sections and the
 * "#" one, the initials avatar, the `show` cells by datatype (an email as a
 * mailto: link with "+n", a reference as its referent's title with the
 * link's role), the local filter narrowing the loaded rows, the trailing
 * transition hidden where the machine does not admit it, a row tap, one
 * trailing action running as a CAS patch, and the facet chips above the
 * filter box only where the mount threads a selection, which the read
 * carries as the property's `eq`. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// The row's trailing button is the foundation's ActionButton, which holds a
// navigate for the `open` verb; no router is mounted here.
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    ...rest
  }: {
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a {...rest}>{children}</a>
  ),
  useNavigate: () => vi.fn(),
}))

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { ViewContext, ViewSpec } from "@/lib/apps/spec"
import ContactsLayout from "./contacts"
import {
  firstAndMore,
  groupContacts,
  initialsOf,
  matchesNeedle,
  sectionOf,
} from "./contacts-sections"

const PERSON = "ada.example.com/people/person"
const ORGANIZATION = "ada.example.com/people/organization"

const person: KindInfo = {
  identity: PERSON,
  name: "person",
  authority: "ada.example.com",
  package: "people",
  version: 4,
  source: "installed",
  description: "",
  definition: {
    names: { singular: "person" },
    displayTemplate: "{displayName|name}",
    properties: {
      name: { type: "string" },
      displayName: { type: "string" },
      emails: { type: "email", repeated: true },
      phones: { type: "string", repeated: true },
      relationship: { type: "enum", values: ["friend", "colleague", "family"] },
      prominence: {
        type: "state",
        states: ["utility", "known"],
        initial: "utility",
        transitions: [
          { from: "utility", to: "known" },
          { from: "known", to: "utility" },
        ],
      },
      memberOf: {
        type: "reference",
        kind: "organization",
        repeated: true,
        properties: {
          role: { type: "string" },
          since: { type: "date" },
        },
      },
    },
  },
}

const organization: KindInfo = {
  identity: ORGANIZATION,
  name: "organization",
  authority: "ada.example.com",
  package: "people",
  version: 4,
  source: "installed",
  description: "",
  definition: {
    names: { singular: "organization" },
    displayTemplate: "{name}",
    properties: { name: { type: "string" }, domain: { type: "string" } },
  },
}

function record(
  id: string,
  properties: Record<string, unknown>,
  kind = PERSON
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 3,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const people = [
  record("ghost", { prominence: "known" }),
  record("fortytwo", { title: "42nd Street Co", prominence: "known" }),
  record("alex", {
    title: "Alex",
    name: "Alex Rivera",
    displayName: "Alex",
    emails: ["alex@acme.example", "alex.rivera@example.org"],
    phones: ["+44 20 7946 0958"],
    memberOf: [
      { ref: `${ORGANIZATION}/acme`, role: "Engineering lead", since: "2023" },
    ],
    prominence: "known",
  }),
  record("bea", {
    title: "Bea Ortiz",
    name: "Bea Ortiz",
    emails: ["bea@example.org"],
    prominence: "known",
  }),
  record("ulysses", {
    title: "Ulysses",
    name: "Ulysses",
    prominence: "utility",
  }),
  record("zed", { title: "Zed", name: "Zed", prominence: "known" }),
]

const spec: ViewSpec = {
  id: "people-contacts",
  name: "Contacts",
  layout: "contacts",
  kind: PERSON,
  requiresAtLeast: {},
  filter: { properties: { prominence: { eq: "known" } } },
  orderBy: [],
  show: ["emails", "phones", "memberOf"],
  facets: [],
  window: {},
  first: 500,
  related: [],
  attach: ["launcher"],
  replaces: false,
  actions: [
    {
      name: "add",
      label: "Add",
      verb: "create",
      placement: "primary",
      prompt: ["name", "emails", "phones"],
      set: {},
      confirm: false,
    },
    {
      name: "demote",
      label: "Demote",
      verb: "transition",
      placement: "row",
      to: "utility",
      prompt: [],
      set: {},
      confirm: false,
    },
  ],
  permissions: { reads: { kinds: [] }, writes: [], call: [], agents: [] },
  problems: [],
}

const calls: { method: string; url: string; body?: unknown }[] = []

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

function stubFetch(over: { people?: SubstrateRecord[]; cursor?: string } = {}) {
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    if (method === "PATCH" && url.includes(`/${PERSON}/`)) {
      const id = decodeURIComponent(url.split("/").pop() ?? "")
      const before = people.find((p) => p.id === id)
      return Promise.resolve(
        json({
          ...before,
          properties: { ...before?.properties, ...body.properties },
          version: (before?.version ?? 0) + 1,
        })
      )
    }
    if (url.includes(`/${PERSON}?`)) {
      const after = new URL(url, "http://x").searchParams.get("after")
      if (after) return Promise.resolve(json({ records: [] }))
      return Promise.resolve(
        json({ records: over.people ?? people, cursor: over.cursor })
      )
    }
    if (url.includes(`/${ORGANIZATION}?`)) {
      return Promise.resolve(
        json({
          records: [
            record(
              "acme",
              { title: "Acme Corp", name: "Acme Corp" },
              ORGANIZATION
            ),
          ],
        })
      )
    }
    return Promise.resolve(
      json({ error: { message: `no stub for ${url}` } }, 404)
    )
  })
}

function renderContacts(
  onOpenRecord = vi.fn(),
  over: { spec?: ViewSpec; ctx?: ViewContext } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const utils = render(
    <NuqsTestingAdapter>
      <QueryClientProvider client={client}>
        <Toaster>
          <ContactsLayout
            spec={over.spec ?? spec}
            kind={person}
            kinds={[person, organization]}
            ctx={over.ctx ?? { inputs: {}, mode: "page" }}
            onOpenRecord={onOpenRecord}
          />
        </Toaster>
      </QueryClientProvider>
    </NuqsTestingAdapter>
  )
  return { ...utils, onOpenRecord }
}

/** jsdom has no matchMedia; `useIsMobile` asks it once per mount. */
function stubViewport(width: number) {
  Object.defineProperty(window, "innerWidth", {
    value: width,
    configurable: true,
    writable: true,
  })
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: width < 768,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  }))
}

beforeEach(() => {
  calls.length = 0
  stubViewport(1024)
  stubFetch()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("contacts sections", () => {
  it("files a title under its initial, accents stripped, and the rest under #", () => {
    expect(sectionOf("Alex")).toBe("A")
    expect(sectionOf("élodie")).toBe("E")
    expect(sectionOf("42nd Street")).toBe("#")
    expect(sectionOf("")).toBe("#")
    expect(sectionOf("王小明")).toBe("#")
  })

  it("groups in index order, empty sections absent, # last", () => {
    const groups = groupContacts(people)
    expect(groups.map((g) => g.key)).toEqual(["A", "B", "U", "Z", "#"])
    expect(groups.at(-1)?.records.map((r) => r.id)).toEqual([
      "ghost",
      "fortytwo",
    ])
  })

  it("takes up to two initials", () => {
    expect(initialsOf("Bea Ortiz")).toBe("BO")
    expect(initialsOf("Alex")).toBe("A")
    expect(initialsOf("Jean Luc Picard")).toBe("JL")
    expect(initialsOf("")).toBe("")
  })

  it("matches the title, the name or an email, case-insensitively", () => {
    const alex = people[2]
    expect(matchesNeedle(alex, "rivera", ["emails"])).toBe(true)
    expect(matchesNeedle(alex, "ACME", ["emails"])).toBe(true)
    expect(matchesNeedle(alex, "acme", [])).toBe(false)
    expect(matchesNeedle(alex, "  ", [])).toBe(true)
  })

  it("reads a repeated value as its first plus the rest", () => {
    expect(firstAndMore(["a", "b", "c"])).toEqual({ first: "a", more: 2 })
    expect(firstAndMore(["", "b"])).toEqual({ first: "b", more: 0 })
    expect(firstAndMore("x")).toEqual({ first: "x", more: 0 })
    expect(firstAndMore(undefined)).toEqual({ first: undefined, more: 0 })
  })
})

describe("ContactsLayout", () => {
  it("sections the page A–Z with a # section for digits and the untitled", async () => {
    renderContacts()
    await screen.findByText("Alex")
    const headers = screen
      .getAllByRole("button", { expanded: true })
      .map((b) => b.textContent?.replace(/\d+$/, ""))
    expect(headers).toEqual(["A", "B", "U", "Z", "#"])
    const other = screen.getByRole("region", { name: "#" })
    expect(within(other).getByText("42nd Street Co")).toBeTruthy()
    expect(within(other).getByText("ghost")).toBeTruthy()
  })

  it("shows an initials avatar per row", async () => {
    const { container } = renderContacts()
    await screen.findByText("Bea Ortiz")
    const initials = [
      ...container.querySelectorAll('[data-slot="avatar-fallback"]'),
    ].map((el) => el.textContent)
    expect(initials).toEqual(["A", "BO", "U", "Z", "G", "4S"])
  })

  it("renders an email as a mailto: link with +n for the rest", async () => {
    renderContacts()
    const link = await screen.findByRole("link", { name: "alex@acme.example" })
    expect(link.getAttribute("href")).toBe("mailto:alex@acme.example")
    expect(link.parentElement?.textContent).toBe("alex@acme.example+1")
    expect(screen.getByText("+44 20 7946 0958")).toBeTruthy()
  })

  it("renders a reference as the referent's title with the link's role", async () => {
    renderContacts()
    expect(await screen.findByText("Acme Corp · Engineering lead")).toBeTruthy()
    // One read of the referenced collection per page, resolved by referents.ts.
    expect(
      calls.filter((c) => c.url.includes(`/${ORGANIZATION}?`))
    ).toHaveLength(1)
  })

  it("narrows the loaded rows as the filter box is typed into", async () => {
    renderContacts()
    await screen.findByText("Alex")
    fireEvent.change(screen.getByLabelText("Filter contacts"), {
      target: { value: "bea" },
    })
    expect(screen.queryByText("Alex")).toBeNull()
    expect(screen.getByText("Bea Ortiz")).toBeTruthy()
    expect(screen.getByText("1 of 6 contacts")).toBeTruthy()
    // an email substring matches too, and nothing matching says so
    fireEvent.change(screen.getByLabelText("Filter contacts"), {
      target: { value: "rivera@example" },
    })
    expect(screen.getByText("Alex")).toBeTruthy()
    fireEvent.change(screen.getByLabelText("Filter contacts"), {
      target: { value: "nobody" },
    })
    expect(screen.getByText("No one matches “nobody”")).toBeTruthy()
    // no second request: the filter is local
    expect(calls.filter((c) => c.url.includes(`/${PERSON}?`))).toHaveLength(1)
  })

  it("offers the row transition only where the machine admits it", async () => {
    renderContacts()
    await screen.findByText("Alex")
    const rowOf = (title: string) =>
      screen.getByText(title).closest("li") as HTMLElement
    expect(
      within(rowOf("Alex")).getByRole("button", { name: "Demote" })
    ).toBeTruthy()
    // a utility person is already there: known → utility is not admitted
    expect(
      within(rowOf("Ulysses")).queryByRole("button", { name: "Demote" })
    ).toBeNull()
    expect(screen.getAllByRole("button", { name: "Demote" })).toHaveLength(5)
  })

  it("reports a row tap through onOpenRecord, but not a mailto tap", async () => {
    const { onOpenRecord } = renderContacts()
    fireEvent.click(await screen.findByText("Zed"))
    expect(onOpenRecord).toHaveBeenCalledTimes(1)
    expect(onOpenRecord.mock.calls[0][0].id).toBe("zed")
    const mailto = screen.getByRole("link", { name: "bea@example.org" })
    // jsdom cannot navigate to a mailto:; only the propagation is under test.
    mailto.addEventListener("click", (e) => e.preventDefault())
    fireEvent.click(mailto)
    expect(onOpenRecord).toHaveBeenCalledTimes(1)
  })

  it("runs the trailing transition as a CAS patch on the row's state", async () => {
    renderContacts()
    const row = (await screen.findByText("Alex")).closest("li") as HTMLElement
    fireEvent.click(within(row).getByRole("button", { name: "Demote" }))
    await waitFor(() => {
      expect(calls.some((c) => c.method === "PATCH")).toBe(true)
    })
    const patch = calls.find((c) => c.method === "PATCH")!
    expect(patch.url).toContain(`/${PERSON}/alex`)
    expect(patch.body).toEqual({
      properties: { prominence: "utility" },
      ifVersion: 3,
    })
  })

  it("walks every page so the index covers the whole collection", async () => {
    stubFetch({ cursor: "next" })
    renderContacts()
    await screen.findByText("Alex")
    await waitFor(() => {
      const reads = calls.filter((c) => c.url.includes(`/${PERSON}?`))
      expect(reads).toHaveLength(2)
      expect(new URL(reads[1].url, "http://x").searchParams.get("after")).toBe(
        "next"
      )
    })
  })

  it("draws the index rail on a phone, live letters only", async () => {
    stubViewport(390)
    renderContacts()
    await screen.findByText("Alex")
    const rail = screen.getByRole("navigation", { name: "Index" })
    expect(
      (
        within(rail).getByRole("button", {
          name: "Jump to A",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(false)
    expect(
      (
        within(rail).getByRole("button", {
          name: "Jump to C",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(true)
  })

  it("says what the view says when the page is empty", async () => {
    stubFetch({ people: [] })
    renderContacts()
    expect(await screen.findByText("No contacts yet")).toBeTruthy()
  })

  it("mounts the facet chips above the filter box only where the mount threads a selection, and the read carries the pick", async () => {
    const faceted = { ...spec, facets: ["relationship"] }
    renderContacts(vi.fn(), {
      spec: faceted,
      ctx: { inputs: {}, mode: "page", facets: { relationship: ["friend"] } },
    })
    await screen.findByText("Zed")
    const narrow = screen.getByRole("group", { name: "Narrow" })
    expect(
      within(narrow)
        .getAllByRole("button")
        .map((b) => b.textContent)
    ).toEqual(["friend", "colleague", "family"])
    const filter = screen.getByLabelText("Filter contacts")
    expect(
      narrow.compareDocumentPosition(filter) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    const read = calls.find((c) => c.url.includes(`/${PERSON}?`))!
    const sent = JSON.parse(
      new URL(read.url, "http://x").searchParams.get("filter") ?? "{}"
    )
    expect(sent.properties.relationship).toEqual({ eq: "friend" })
    expect(sent.properties.prominence).toEqual({ eq: "known" })

    cleanup()
    calls.length = 0
    renderContacts(vi.fn(), { spec: faceted })
    await screen.findByText("Zed")
    expect(screen.queryByRole("group", { name: "Narrow" })).toBeNull()
  })
})
