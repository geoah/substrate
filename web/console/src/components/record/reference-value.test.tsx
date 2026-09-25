// @vitest-environment jsdom
/** A reference in a TABLE CELL. Every reference column flattened its value
 * through `cellValue`, which summarizes an object by its keys — so the agents
 * table printed the literal `{ref}` where the provider row belonged. The cell
 * is the referent's pill, and it is a link. */

import { cleanup, render } from "@testing-library/react"
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

import { ReferenceCell, ReferenceValue } from "./reference-value"
import type { KindInfo } from "@/lib/api/types"
import type { ReferenceTitles } from "@/lib/reference-titles"

function kind(identity: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "builtin",
    description: "",
    definition: { properties: {} },
  }
}

const registry = [
  kind("substrate.reamde.dev/llm/provider"),
  kind("samples.substrate.reamde.dev/tasks/task"),
]

afterEach(cleanup)

describe("a reference in a cell", () => {
  it("names the referent and links to it, never the served shape's key", () => {
    const { container } = render(
      <ReferenceCell
        value={{ ref: "substrate.reamde.dev/llm/provider/default" }}
        kinds={registry}
      />
    )
    // Never a bare id: an untitled referent is named by its kind.
    expect(container.textContent).toBe("Untitled provider")
    expect(container.textContent).not.toContain("{ref}")
    const link = container.querySelector("a")
    expect(link?.getAttribute("href")).toBe(
      "/data/substrate.reamde.dev/llm/provider/default"
    )
  })

  it("reads the authored string shorthand as the same pill", () => {
    const { container } = render(
      <ReferenceCell
        value="substrate.reamde.dev/llm/provider/default"
        kinds={registry}
      />
    )
    expect(container.querySelector("a")?.getAttribute("href")).toBe(
      "/data/substrate.reamde.dev/llm/provider/default"
    )
  })

  it("gives a repeated reference one pill per referent, in stored order", () => {
    const { container } = render(
      <ReferenceCell
        value={[
          { ref: "samples.substrate.reamde.dev/tasks/task/one" },
          { ref: "samples.substrate.reamde.dev/tasks/task/two" },
        ]}
        kinds={registry}
      />
    )
    const links = [...container.querySelectorAll("a")]
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "/data/samples.substrate.reamde.dev/tasks/task/one",
      "/data/samples.substrate.reamde.dev/tasks/task/two",
    ])
  })

  // A reference may name a kind nobody installed: there is no page to link to,
  // and inventing one would 404. The path still reads.
  it("shows the path, unlinked, for a kind the registry does not have", () => {
    const { container } = render(
      <ReferenceCell
        value={{ ref: "ada.example.com/crm/lead/7" }}
        kinds={registry}
      />
    )
    expect(container.querySelector("a")).toBeNull()
    expect(container.textContent).toBe("ada.example.com/crm/lead/7")
  })

  // The row under the cell is clickable; a pill that let the click through
  // would navigate to the referent and then have the row navigate over it.
  it("keeps the click off the row beneath it", () => {
    const onRow = vi.fn()
    const { container } = render(
      <div onClick={onRow}>
        <ReferenceCell
          value={{ ref: "substrate.reamde.dev/llm/provider/default" }}
          kinds={registry}
        />
      </div>
    )
    const link = container.querySelector("a")
    link?.dispatchEvent(new MouseEvent("click", { bubbles: true }))
    expect(onRow).not.toHaveBeenCalled()
  })

  it("renders nothing at all when the record holds no reference", () => {
    const { container } = render(<ReferenceCell value={[]} kinds={registry} />)
    expect(container.textContent).toBe("")
  })
})

/** What a pointer is CALLED. A stored reference carries the referent's path
 * and nothing else, so the pill reads as a record id until the surface
 * resolves the title — the list's `included` sidecar on a table, one batched
 * read on a record page — and hands the resolver down. */
describe("a reference wearing its referent's title", () => {
  const titles: ReferenceTitles = new Map([
    ["samples.substrate.reamde.dev/tasks/task/one", "Buy milk"],
  ])

  it("labels the cell's pill with the title, keeping the link", () => {
    const { container } = render(
      <ReferenceCell
        value={{ ref: "samples.substrate.reamde.dev/tasks/task/one" }}
        kinds={registry}
        titles={titles}
      />
    )
    expect(container.textContent).toBe("Buy milk")
    const link = container.querySelector("a")
    expect(link?.getAttribute("href")).toBe(
      "/data/samples.substrate.reamde.dev/tasks/task/one"
    )
  })

  it("names a path the resolver does not answer by its kind, never its id", () => {
    const { container } = render(
      <ReferenceCell
        value={{ ref: "samples.substrate.reamde.dev/tasks/task/two" }}
        kinds={registry}
        titles={titles}
      />
    )
    expect(container.textContent).toBe("Untitled task")
  })

  it("titles the record page's value and keeps its link data beside it", () => {
    const { container } = render(
      <ReferenceValue
        value={{
          ref: "samples.substrate.reamde.dev/tasks/task/one",
          role: "blocks",
        }}
        kinds={registry}
        titles={titles}
      />
    )
    expect(container.textContent).toContain("Buy milk")
    expect(container.textContent).toContain("role: blocks")
  })

  // A kind nobody installed has no page and is never read for a title; the
  // resolver cannot speak for it and the path stays inert text.
  it("stays inert text for a kind the registry does not have", () => {
    const { container } = render(
      <ReferenceCell
        value={{ ref: "ada.example.com/crm/lead/7" }}
        kinds={registry}
        titles={new Map([["ada.example.com/crm/lead/7", "Big Fish"]])}
      />
    )
    expect(container.querySelector("a")).toBeNull()
    expect(container.textContent).toBe("ada.example.com/crm/lead/7")
  })

  // A surface that resolved nothing hands nothing down; with no query client
  // to read the title either, the referent is named by its kind.
  it("reads as untitled when the surface resolved no titles at all", () => {
    const { container } = render(
      <ReferenceValue
        value={{ ref: "samples.substrate.reamde.dev/tasks/task/one" }}
        kinds={registry}
      />
    )
    expect(container.textContent).toBe("Untitled task")
  })
})
