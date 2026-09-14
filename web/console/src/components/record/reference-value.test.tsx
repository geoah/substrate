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

import { ReferenceCell } from "./reference-value"
import type { KindInfo } from "@/lib/api/types"

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
    expect(container.textContent).toContain("default")
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
    expect(links.map((a) => a.textContent)).toEqual(["one", "two"])
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
