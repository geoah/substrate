// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({
    to,
    children,
    ...rest
  }: {
    to: string
    children?: React.ReactNode
    [key: string]: unknown
  }) => {
    const props = { ...rest }
    delete props.search
    return (
      <a href={to} {...props}>
        {children}
      </a>
    )
  },
}))

import { LandingPage } from "./landing-page"

const SITE = {
  name: "Crane",
  tagline: "A home for your people",
  logo: "/site/logo.svg",
  landing: "/site/landing.html",
}

const FRAGMENT = `<style>h1{color:var(--primary)}</style>
<h1>Your own corner</h1>
<script>window.ran = true</script>
<a id="join" href="/register">Create your Substrate</a>
<a id="about" href="https://elsewhere.example/">Read more</a>
<a id="help" href="/login" target="help">Help</a>`

function fragmentRoot(): ShadowRoot {
  const host = document.querySelector('[data-slot="landing-fragment"]')
  return host!.shadowRoot!
}

describe("LandingPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    navigate.mockClear()
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("frames the fragment with the brand and the two doors", async () => {
    fetchMock.mockResolvedValue(new Response(FRAGMENT, { status: 200 }))
    render(<LandingPage site={SITE} />)
    expect(screen.getByText("Crane")).toBeTruthy()
    expect(document.querySelector('img[src="/site/logo.svg"]')).not.toBeNull()
    expect(
      screen.getByRole("link", { name: "Sign in" }).getAttribute("href")
    ).toBe("/login")
    expect(
      screen
        .getByRole("link", { name: "Create your Substrate" })
        .getAttribute("href")
    ).toBe("/register")
    await waitFor(() =>
      expect(fragmentRoot().querySelector("h1")).not.toBeNull()
    )
    expect(String(fetchMock.mock.calls[0][0])).toBe("/site/landing.html")
    expect(fragmentRoot().querySelector("h1")!.textContent).toBe(
      "Your own corner"
    )
    // Mounted as markup: the fragment's script is gone and never ran.
    expect(fragmentRoot().querySelector("script")).toBeNull()
    expect((window as unknown as { ran?: boolean }).ran).toBeUndefined()
  })

  it("opens a door the fragment links to inside the console", async () => {
    fetchMock.mockResolvedValue(new Response(FRAGMENT, { status: 200 }))
    render(<LandingPage site={SITE} />)
    await waitFor(() =>
      expect(fragmentRoot().querySelector("#join")).not.toBeNull()
    )
    const join = fragmentRoot().querySelector<HTMLAnchorElement>("#join")!
    const click = new MouseEvent("click", {
      bubbles: true,
      cancelable: true,
      composed: true,
      button: 0,
    })
    join.dispatchEvent(click)
    expect(click.defaultPrevented).toBe(true)
    expect(navigate).toHaveBeenCalledWith({ href: "/register" })

    const about = fragmentRoot().querySelector<HTMLAnchorElement>("#about")!
    const away = new MouseEvent("click", {
      bubbles: true,
      cancelable: true,
      composed: true,
      button: 0,
    })
    // jsdom does not navigate; the point is that the console left it alone.
    about.addEventListener("click", (e) => e.preventDefault())
    navigate.mockClear()
    about.dispatchEvent(away)
    expect(navigate).not.toHaveBeenCalled()

    // A door link meant for another window stays the browser's.
    const help = fragmentRoot().querySelector<HTMLAnchorElement>("#help")!
    help.addEventListener("click", (e) => e.preventDefault())
    help.dispatchEvent(
      new MouseEvent("click", {
        bubbles: true,
        cancelable: true,
        composed: true,
        button: 0,
      })
    )
    expect(navigate).not.toHaveBeenCalled()
  })

  it("falls back to the name and the doors when the fragment is missing", async () => {
    fetchMock.mockResolvedValue(new Response("not found", { status: 404 }))
    render(<LandingPage site={SITE} />)
    await screen.findByRole("heading", { name: "Crane" })
    expect(screen.getByText("A home for your people")).toBeTruthy()
    expect(screen.getAllByRole("link", { name: "Sign in" }).length).toBe(2)
    expect(
      screen.getAllByRole("link", { name: "Create your Substrate" }).length
    ).toBe(2)
  })
})
