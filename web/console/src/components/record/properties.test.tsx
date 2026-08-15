/** The record's data, read as data (#38): one row per DECLARED property,
 * rendered by datatype rather than dumped as YAML.
 *
 * What the declaration owes the reader: prose with its line breaks intact, a
 * datetime as a date, a pointer as a link, an enum as its authored LABEL, a
 * secret said to be unreadable rather than shown blank, and an unset property
 * present and dimmed instead of silently missing. */

import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

// The house pattern for a component that renders <Link>: mock the router
// rather than stand one up, and assert on the route and params the link was
// GIVEN. Where it navigates is the router's contract, not this component's.
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params: linkParams,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(linkParams ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { PropertiesRail } from "./properties"

const article: KindInfo = {
  identity: "blog.example.com/article",
  name: "article",
  authority: "blog.example.com",
  version: 1,
  plural: "articles",
  source: "installed",
  definition: {
    properties: {
      headline: { type: "string", description: "what it is called" },
      body: { type: "text" },
      status: {
        type: "enum",
        values: [{ value: "inReview", label: "In review" }, { value: "live" }],
      },
      readingMinutes: { type: "int" },
      publishedAt: { type: "datetime" },
      author: { type: "reference", kind: "people.example.com/person" },
      apiKey: { type: "secret" },
      subtitle: { type: "string" },
    },
    edges: {
      related: { to: "article", many: true },
    },
  },
}

const person: KindInfo = {
  identity: "people.example.com/person",
  name: "person",
  authority: "people.example.com",
  version: 1,
  plural: "people",
  source: "installed",
  definition: { properties: {} },
}

function record(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "a1",
    kind: "blog.example.com/article",
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-08-01T10:00:00Z",
    updatedAt: "2026-08-01T10:00:00Z",
  }
}

function renderRail(e: SubstrateRecord) {
  return render(
    <PropertiesRail record={e} kind={article} kinds={[article, person]} />
  )
}

/** What a mocked Link was asked to navigate to, as one path. */
function linkTarget(el: HTMLElement): string {
  const params = JSON.parse(el.getAttribute("data-params") ?? "{}") as Record<
    string,
    string
  >
  return (el.getAttribute("data-to") ?? "").replace(
    /\$(\w+)/g,
    (_, k: string) => params[k] ?? ""
  )
}

afterEach(cleanup)

describe("PropertiesRail", () => {
  it("keeps a text property's line breaks, rather than folding it onto one line", () => {
    renderRail(record({ body: "First line.\nSecond line." }))
    const prose = screen.getByText(/First line/)
    expect(prose.textContent).toBe("First line.\nSecond line.")
    // The class is what preserves them; without it the browser collapses the
    // newline and the two lines run together.
    expect(prose.className).toContain("whitespace-pre-wrap")
  })

  it("shows an enum's authored label, and the stored value beside it", () => {
    renderRail(record({ status: "inReview" }))
    expect(screen.getByText("In review")).toBeTruthy()
    // The raw value stays visible: the label is for reading, the value is what
    // a filter or an apply would carry.
    expect(screen.getByText("inReview")).toBeTruthy()
  })

  it("humanizes an enum value the declaration gave no label", () => {
    renderRail(record({ status: "live" }))
    expect(screen.getByText("Live")).toBeTruthy()
  })

  it("reads a datetime as a date, not an ISO string", () => {
    renderRail(record({ publishedAt: "2026-08-01T10:00:00Z" }))
    expect(screen.queryByText("2026-08-01T10:00:00Z")).toBeNull()
    expect(screen.getByText(/2026-08-01 /)).toBeTruthy()
  })

  it("links a pointer to the record it names", () => {
    renderRail(record({ author: "people.example.com/person/ada" }))
    const link = screen.getByText("ada")
    expect(linkTarget(link)).toBe("/data/people.example.com/people/ada")
  })

  it("shows a pointer that is not a path as the stored string", () => {
    // An unresolvable pointer is still the stored fact. Inventing a link for
    // it would be worse than showing what is there.
    renderRail(record({ author: "not-a-path" }))
    const shown = screen.getByText("not-a-path")
    expect(shown.tagName).not.toBe("A")
  })

  it("says a secret is unreadable rather than showing it blank", () => {
    // Blank would read as "unset", which is a different and wrong fact: the
    // value exists, the read just never serves it.
    renderRail(record({ apiKey: "whatever-the-store-holds" }))
    expect(screen.getByText(/write-only/)).toBeTruthy()
    expect(screen.queryByText("whatever-the-store-holds")).toBeNull()
  })

  it("shows every declared property, unset ones included", () => {
    renderRail(record({ headline: "Hello" }))
    // Present.
    expect(screen.getByText("Headline")).toBeTruthy()
    // Declared but unset — still listed, so the record reads against the
    // shape of its kind rather than as the subset that happens to hold data.
    expect(screen.getByText("Subtitle")).toBeTruthy()
    // Sentence case, per humanizeName: "Reading minutes", not title case.
    expect(screen.getByText("Reading minutes")).toBeTruthy()
  })

  it("surfaces a property the declaration does not name", () => {
    // Hiding it would make this view lie about being all the record's data.
    renderRail(record({ headline: "Hello", legacyField: "still here" }))
    expect(screen.getByText("Not declared")).toBeTruthy()
    expect(screen.getByText("still here")).toBeTruthy()
  })

  it("lists edge targets by title, linked", () => {
    const e = record({ headline: "Hello" })
    e.edges = {
      related: [
        { id: "a2", kind: "blog.example.com/article", title: "The other one" },
      ],
    }
    renderRail(e)
    const link = screen.getByText("The other one")
    expect(linkTarget(link)).toBe("/data/blog.example.com/articles/a2")
  })
})
