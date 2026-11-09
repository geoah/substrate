import { useSyncExternalStore } from "react"

/** Where the owner is, in the address bar's hash, so a link opens the same
 * screen and Back goes back. A screen's filters ride in the hash's query
 * (`#/labs?status=out`). */
export type Route =
  | { screen: "today" }
  | { screen: "labs"; query: string }
  | { screen: "lab"; entry: string; query: string }
  | { screen: "reports" }
  | { screen: "report"; id: string }
  | { screen: "healthspan"; query: string }
  | { screen: "healthspanMetric"; id: string }
  | { screen: "how" }
  | { screen: "record"; query: string }
  | { screen: "sources" }
  | { screen: "source"; id: string }
  | { screen: "import"; id?: string }
  | { screen: "settings" }
  | { screen: "start" }

/** The top-level sections the tab bar and the sidebar show. */
export type Section =
  "today" | "labs" | "healthspan" | "record" | "sources" | "settings"

export function sectionOf(route: Route): Section {
  switch (route.screen) {
    case "labs":
    case "lab":
    case "reports":
    case "report":
      return "labs"
    case "healthspan":
    case "healthspanMetric":
    case "how":
      return "healthspan"
    case "record":
      return "record"
    case "sources":
    case "source":
    case "import":
      return "sources"
    case "settings":
    case "start":
      return "settings"
    case "today":
      return "today"
  }
}

export function parseRoute(hash: string): Route {
  const raw = hash.replace(/^#\/?/, "")
  const q = raw.indexOf("?")
  const path = q < 0 ? raw : raw.slice(0, q)
  const query = q < 0 ? "" : raw.slice(q + 1)
  const parts = path.split("/").filter(Boolean).map(decodeURIComponent)
  const [first, second, third] = parts
  switch (first) {
    case "labs":
      if (second === "reports")
        return third ? { screen: "report", id: third } : { screen: "reports" }
      return second
        ? { screen: "lab", entry: second, query }
        : { screen: "labs", query }
    case "healthspan":
      if (second === "how") return { screen: "how" }
      return second
        ? { screen: "healthspanMetric", id: second }
        : { screen: "healthspan", query }
    case "record":
      return { screen: "record", query }
    case "sources":
      if (second === "import")
        return third ? { screen: "import", id: third } : { screen: "import" }
      return second ? { screen: "source", id: second } : { screen: "sources" }
    case "settings":
      return { screen: "settings" }
    case "start":
      return { screen: "start" }
    default:
      return { screen: "today" }
  }
}

const seg = encodeURIComponent
const withQuery = (path: string, query: string) =>
  query ? `${path}?${query}` : path

export function routeHash(route: Route): string {
  switch (route.screen) {
    case "today":
      return "#/"
    case "labs":
      return withQuery("#/labs", route.query)
    case "lab":
      return withQuery(`#/labs/${seg(route.entry)}`, route.query)
    case "reports":
      return "#/labs/reports"
    case "report":
      return `#/labs/reports/${seg(route.id)}`
    case "healthspan":
      return withQuery("#/healthspan", route.query)
    case "healthspanMetric":
      return `#/healthspan/${seg(route.id)}`
    case "how":
      return "#/healthspan/how"
    case "record":
      return withQuery("#/record", route.query)
    case "sources":
      return "#/sources"
    case "source":
      return `#/sources/${seg(route.id)}`
    case "import":
      return route.id ? `#/sources/import/${seg(route.id)}` : "#/sources/import"
    case "settings":
      return "#/settings"
    case "start":
      return "#/start"
  }
}

/** Moves to a route; `replace` leaves no history entry (a filter change). */
export function go(route: Route, replace = false): void {
  const hash = routeHash(route)
  if (replace) {
    const url = new URL(window.location.href)
    url.hash = hash
    window.history.replaceState(window.history.state, "", url)
    window.dispatchEvent(new HashChangeEvent("hashchange"))
  } else {
    window.location.hash = hash
  }
}

function subscribe(fn: () => void): () => void {
  window.addEventListener("hashchange", fn)
  return () => window.removeEventListener("hashchange", fn)
}

export function useRoute(): Route {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash)
  return parseRoute(hash)
}
