// @vitest-environment jsdom
/** The react arm's document: the import map carries the build's five
 * specifiers and one `#name` per authored module, the entry is imported by
 * its own blob URL and is never a key of the map, so an authored module
 * cannot take its place, and one spelled as the entry is refused before
 * anything is written. The html arm writes the map it is given. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { LEFT_TYPE, METHODS } from "@/lib/apps/bridge/protocol"
import { mountHtml, mountReact } from "./load"

const IMPORTS = {
  react: "/assets/app-react-Ab12Cd34.js",
  "react/jsx-runtime": "/assets/app-jsx-runtime-Ef56Gh78.js",
  "react-dom/client": "/assets/app-react-dom-client-Ij90Kl12.js",
  "substrate/app": "/assets/app-sdk-Mn34Op56.js",
  "substrate/ui": "/assets/app-ui-Qr78St90.js",
}

const ENTRY = "export default function App() { return <p>entry</p> }\n"
const HELPER = "export const n = 1\n"

/** What the shell wrote, parsed back: the import map and the URL the
 * bootstrap imports. */
function written(html: string) {
  const map = /<script type="importmap">(.*?)<\/script>/.exec(html)
  const boot = /const mod = await import\("([^"]+)"\)/.exec(html)
  return {
    imports: map ? (JSON.parse(map[1]).imports as Record<string, string>) : {},
    entry: boot?.[1],
  }
}

describe("the shell's document", () => {
  let html: string
  let port: { postMessage: ReturnType<typeof vi.fn> }
  let minted: string[]

  beforeEach(() => {
    html = ""
    minted = []
    port = { postMessage: vi.fn() }
    // jsdom has no object URLs; the shell needs one distinct string per blob.
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: () => {
        const url = `blob:null/${minted.length + 1}`
        minted.push(url)
        return url
      },
    })
    vi.spyOn(document, "open").mockImplementation(
      (() => document) as typeof document.open
    )
    vi.spyOn(document, "write").mockImplementation((s: string) => {
      html += s
    })
    vi.spyOn(document, "close").mockImplementation(() => {})
  })

  afterEach(() => {
    delete (URL as { createObjectURL?: unknown }).createObjectURL
    vi.restoreAllMocks()
  })

  it("keeps the entry outside the map, so no module can replace it", async () => {
    await mountReact(port as unknown as MessagePort, IMPORTS, {
      source: ENTRY,
      modules: { helper: HELPER },
      nonce: "n1",
    })
    const doc = written(html)
    expect(doc.imports).toEqual({ ...IMPORTS, "#helper": minted[1] })
    expect(doc.entry).toBe(minted[0])
    expect(Object.values(doc.imports)).not.toContain(doc.entry)
    expect(doc.imports["#source"]).toBeUndefined()
    expect(port.postMessage).not.toHaveBeenCalled()
  })

  it("refuses a module spelled as the entry before writing anything", async () => {
    await mountReact(port as unknown as MessagePort, IMPORTS, {
      source: ENTRY,
      modules: { source: HELPER },
      nonce: "n1",
    })
    expect(html).toBe("")
    expect(port.postMessage).toHaveBeenCalledTimes(1)
    const [msg] = port.postMessage.mock.calls[0]
    expect(msg.method).toBe(METHODS.error)
    expect(msg.params).toMatchObject({
      phase: "transform",
      module: "source",
    })
  })

  it("reports a transform error in the module it is in and writes nothing", async () => {
    await mountReact(port as unknown as MessagePort, IMPORTS, {
      source: ENTRY,
      modules: { broken: "export const x = (" },
      nonce: "n1",
    })
    expect(html).toBe("")
    const [msg] = port.postMessage.mock.calls[0]
    expect(msg.params).toMatchObject({ phase: "transform", module: "broken" })
  })

  it("tells the host when its document is unloaded, from a listener added after the open", () => {
    const opened: string[] = []
    vi.mocked(document.open).mockImplementation((() => {
      opened.push("open")
      return document
    }) as typeof document.open)
    const added = vi.spyOn(window, "addEventListener")
    const posted = vi
      .spyOn(window.parent, "postMessage")
      .mockImplementation(() => {})
    mountHtml(port as unknown as MessagePort, IMPORTS, "<p>hi</p>", "n2")
    const pagehide = added.mock.calls.findIndex(([type]) => type === "pagehide")
    expect(pagehide).toBeGreaterThanOrEqual(0)
    expect(opened).toEqual(["open"])
    window.dispatchEvent(new Event("pagehide"))
    expect(posted).toHaveBeenCalledWith({ type: LEFT_TYPE, nonce: "n2" }, "*")
  })

  it("writes the map it is given before an html source", () => {
    mountHtml(port as unknown as MessagePort, IMPORTS, "<p>hi</p>", "n3")
    expect(written(html).imports).toEqual(IMPORTS)
    expect(html.startsWith('<!doctype html><script type="importmap">')).toBe(
      true
    )
    expect(html.endsWith("<p>hi</p>")).toBe(true)
  })
})
