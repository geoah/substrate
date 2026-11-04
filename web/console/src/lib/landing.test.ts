// @vitest-environment jsdom
import { describe, expect, it } from "vitest"

import { doorPath, landingFragment } from "./landing"

function html(fragment: DocumentFragment): string {
  const div = document.createElement("div")
  div.append(fragment)
  return div.innerHTML
}

describe("landingFragment", () => {
  it("keeps markup and styles", () => {
    const out = html(
      landingFragment(
        '<style>h1{color:var(--primary)}</style><h1 class="t">Hi</h1><a href="/register">Join</a>'
      )
    )
    expect(out).toContain("<style>h1{color:var(--primary)}</style>")
    expect(out).toContain('<h1 class="t">Hi</h1>')
    expect(out).toContain('<a href="/register">Join</a>')
  })

  it("drops what would run code or load another document", () => {
    const out = html(
      landingFragment(
        '<script>alert(1)</script><img src="x" onerror="alert(1)"><a href="javascript:alert(1)">x</a><iframe src="/"></iframe><base href="https://evil.example/">'
      )
    )
    expect(out).not.toMatch(/script|onerror|javascript:|iframe|base/i)
    expect(out).toContain('<img src="/x">')
  })

  it("resolves relative URLs against the landing file", () => {
    const out = html(
      landingFragment(
        '<img src="logo.svg"><img src="../up.png"><a href="#top">t</a><a href="/login">in</a><a href="https://elsewhere.example/">x</a>',
        "/site/pages/landing.html"
      )
    )
    expect(out).toContain('<img src="/site/pages/logo.svg">')
    expect(out).toContain('<img src="/site/up.png">')
    expect(out).toContain('<a href="#top">t</a>')
    expect(out).toContain('<a href="/login">in</a>')
    expect(out).toContain('<a href="https://elsewhere.example/">x</a>')
  })
})

describe("doorPath", () => {
  const origin = "https://crane.example"
  it("takes over the two doors on this server", () => {
    expect(doorPath("/login", origin)).toBe("/login")
    expect(doorPath("/register?x=1", origin)).toBe("/register?x=1")
    expect(doorPath("https://crane.example/register", origin)).toBe("/register")
  })

  it("leaves every other link to the browser", () => {
    expect(doorPath("/about", origin)).toBeNull()
    expect(doorPath("https://elsewhere.example/login", origin)).toBeNull()
    expect(doorPath("mailto:me@crane.example", origin)).toBeNull()
  })
})
