import { useEffect, useRef, useState } from "react"
import { Link, useNavigate } from "@tanstack/react-router"

import { DoorBrand } from "@/components/door/door-layout"
import { buttonVariants } from "@/components/ui/button"
import { doorPath, landingFragment } from "@/lib/landing"
import { useDocumentTitle } from "@/hooks/use-document-title"
import type { SiteInfo } from "@/lib/site"

/** What the fragment's shadow root starts with. The console's tokens are
 * custom properties on the document, and custom properties inherit through
 * the shadow boundary, so the fragment reads `var(--primary)` and follows the
 * theme without the console's stylesheet reaching in. */
const HOST_STYLE = `:host { display: block; color: var(--foreground); font-family: inherit; line-height: 1.5; }
*, *::before, *::after { box-sizing: border-box; }
a { color: var(--primary-text); }
img, svg { max-width: 100%; }`

type Load =
  { state: "loading" } | { state: "ready"; html: string } | { state: "failed" }

/** Fetch the landing fragment once. A missing or unreadable file is not an
 * error page: the frame falls back to the site's name and the two doors. */
function useLanding(path: string): Load {
  const [load, setLoad] = useState<Load>({ state: "loading" })
  useEffect(() => {
    let live = true
    fetch(path, { headers: { Accept: "text/html" } })
      .then(async (res) => {
        if (!res.ok) throw new Error(String(res.status))
        const html = await res.text()
        if (live) setLoad({ state: "ready", html })
      })
      .catch(() => {
        if (live) setLoad({ state: "failed" })
      })
    return () => {
      live = false
    }
  }, [path])
  return load
}

/** The fragment, mounted in a shadow root so its styles stay its own. A click
 * on a link to `/login` or `/register` opens that door inside the console
 * instead of reloading the page. */
function LandingFragment({ html, source }: { html: string; source: string }) {
  const host = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()
  useEffect(() => {
    const el = host.current
    if (!el) return
    const root = el.shadowRoot ?? el.attachShadow({ mode: "open" })
    const style = document.createElement("style")
    style.textContent = HOST_STYLE
    root.replaceChildren(style, landingFragment(html, source))
    function onClick(event: Event) {
      const e = event as MouseEvent
      if (e.defaultPrevented || e.button !== 0) return
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
      const anchor = e
        .composedPath()
        .find((n): n is HTMLAnchorElement => n instanceof HTMLAnchorElement)
      // A download, or a link meant for another window, stays the browser's.
      if (!anchor || anchor.hasAttribute("download")) return
      if (anchor.target && anchor.target !== "_self") return
      const path = doorPath(
        anchor.getAttribute("href") ?? "",
        window.location.origin
      )
      if (!path) return
      e.preventDefault()
      void navigate({ href: path })
    }
    root.addEventListener("click", onClick)
    return () => root.removeEventListener("click", onClick)
  }, [html, source, navigate])
  return <div ref={host} data-slot="landing-fragment" />
}

/** What a signed-out visitor sees at `/` when the deployment sets a landing
 * page: a thin frame the console owns (the brand, and the two doors) around
 * the operator's fragment. */
export function LandingPage({ site }: { site: SiteInfo }) {
  useDocumentTitle(site.tagline ? `${site.name}: ${site.tagline}` : site.name)
  const load = useLanding(site.landing ?? "")
  return (
    <div className="min-h-svh bg-background">
      <header className="sticky top-0 z-10 border-b border-transparent bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/70">
        <div className="mx-auto flex h-16 w-full max-w-5xl items-center justify-between gap-3 px-4 sm:px-6">
          <Link to="/" className="rounded-lg" aria-label={site.name}>
            <DoorBrand site={site} size="sm" />
          </Link>
          <nav className="flex items-center gap-1.5 sm:gap-2">
            <Link
              to="/login"
              search={{}}
              className={buttonVariants({ variant: "ghost", size: "lg" })}
            >
              Sign in
            </Link>
            <Link
              to="/register"
              search={{}}
              className={buttonVariants({ size: "lg" })}
            >
              Create your Substrate
            </Link>
          </nav>
        </div>
      </header>
      <main>
        {load.state === "ready" && (
          <LandingFragment html={load.html} source={site.landing ?? "/"} />
        )}
        {load.state === "failed" && (
          <section className="mx-auto flex max-w-xl flex-col items-center gap-4 px-4 py-24 text-center">
            <h1 className="text-3xl font-semibold tracking-tight">
              {site.name}
            </h1>
            {site.tagline && (
              <p className="text-lg text-muted-foreground">{site.tagline}</p>
            )}
            <div className="mt-2 flex gap-2">
              <Link
                to="/register"
                search={{}}
                className={buttonVariants({ size: "lg" })}
              >
                Create your Substrate
              </Link>
              <Link
                to="/login"
                search={{}}
                className={buttonVariants({ variant: "outline", size: "lg" })}
              >
                Sign in
              </Link>
            </div>
          </section>
        )}
      </main>
    </div>
  )
}
