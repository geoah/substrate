// @vitest-environment jsdom
/** The doors and an app's approval page (decision 0194): a signed-out owner
 * on `/authorize/$request` goes through `/login` and comes back by the page's
 * plain path, and a signed-in one on either door goes where the door would
 * have sent them, but only to a path on this console. */

import { afterEach, describe, expect, it } from "vitest"

import { clearSession, saveSession } from "@/lib/api/session"
import { authorizeRoute, loginRoute, registerRoute } from "@/router"

/** What a route's beforeLoad redirects to; undefined when it lets the page
 * render. */
function target(
  route: { options: { beforeLoad?: unknown } },
  params: Record<string, string> = {},
  search: Record<string, unknown> = {}
): unknown {
  const beforeLoad = route.options.beforeLoad as (ctx: unknown) => void
  try {
    beforeLoad({ params, search, location: { search } })
  } catch (thrown) {
    const r = thrown as { options?: Record<string, unknown> }
    const { to, search: s } = r.options ?? {}
    return { to, search: s }
  }
  return undefined
}

function signIn() {
  saveSession("substrate_tok_owner", "ada.example.com", "tok-owner")
}

afterEach(() => clearSession())

describe("the approval page's gate", () => {
  it("sends a signed-out owner to sign in, returning by the plain path", () => {
    clearSession()
    expect(target(authorizeRoute, { request: "AbC123-_x" })).toEqual({
      to: "/login",
      search: { redirect: "/authorize/AbC123-_x" },
    })
  })

  it("lets a signed-in owner through", () => {
    signIn()
    expect(target(authorizeRoute, { request: "AbC123" })).toBeUndefined()
  })
})

describe.each([
  ["/login", loginRoute],
  ["/register", registerRoute],
])("%s with a live session", (_path, route) => {
  it("goes to the return path", () => {
    signIn()
    expect(target(route, {}, { redirect: "/authorize/AbC123" })).toMatchObject({
      to: "/authorize/AbC123",
    })
  })

  it("goes Home without one, or when it leaves the console", () => {
    signIn()
    expect(target(route)).toMatchObject({ to: "/" })
    expect(target(route, {}, { redirect: "//evil.example" })).toMatchObject({
      to: "/",
    })
    expect(
      target(route, {}, { redirect: "https://evil.example" })
    ).toMatchObject({ to: "/" })
  })

  it("renders for a signed-out reader", () => {
    clearSession()
    expect(target(route, {}, { redirect: "/authorize/AbC123" })).toBeUndefined()
  })

  it("keeps the return path through validation", () => {
    const validate = route.options.validateSearch as (
      s: Record<string, unknown>
    ) => { redirect?: string }
    expect(validate({ redirect: "/authorize/AbC123" })).toEqual({
      redirect: "/authorize/AbC123",
    })
    expect(validate({ redirect: 42 })).toEqual({ redirect: undefined })
  })
})
