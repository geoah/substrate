import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import { Markdown } from "./markdown"

function render(text: string): HTMLElement {
  const container = document.createElement("div")
  container.innerHTML = renderToStaticMarkup(<Markdown text={text} />)
  return container
}

describe("notes as markdown", () => {
  it("draws the subset as elements", () => {
    const notes = render(
      "# Plan\n\n**bold** and *it*\n\n- [x] done\n- open\n\n> quoted"
    )
    expect(notes.querySelector("strong")?.textContent).toBe("bold")
    expect(notes.querySelector("em")?.textContent).toBe("it")
    expect(notes.querySelectorAll("li")).toHaveLength(2)
    expect(notes.querySelector("blockquote")?.textContent).toBe("quoted")
  })

  it("runs nothing a note holds and links only web and mail addresses", () => {
    const notes = render(
      "<img src=x onerror=alert(1)> [a](javascript:alert(1)) [b](https://example.com/a(b)) [c](mailto:x@example.com)"
    )
    expect(notes.querySelector("img")).toBeNull()
    const links = [...notes.querySelectorAll("a")].map((a) =>
      a.getAttribute("href")
    )
    expect(links).toEqual(["https://example.com/a(b)", "mailto:x@example.com"])
    expect(notes.textContent).toContain("<img src=x onerror=alert(1)>")
  })
})
