import { useSyncExternalStore } from "react"

/** Where the owner is, in the address bar's hash, so a link to an item
 * opens it and Back goes back. */
export type Route =
  { screen: "items" } | { screen: "item"; id: string } | { screen: "settings" }

export function parseRoute(hash: string): Route {
  const [first, second] = hash
    .replace(/^#\/?/, "")
    .split("/")
    .filter(Boolean)
    .map(decodeURIComponent)
  if (first === "item" && second) return { screen: "item", id: second }
  if (first === "settings") return { screen: "settings" }
  return { screen: "items" }
}

export function routeHash(route: Route): string {
  switch (route.screen) {
    case "item":
      return `#/item/${encodeURIComponent(route.id)}`
    case "settings":
      return "#/settings"
    case "items":
      return "#/"
  }
}

function subscribe(fn: () => void): () => void {
  window.addEventListener("hashchange", fn)
  return () => window.removeEventListener("hashchange", fn)
}

export function useRoute(): [Route, (route: Route) => void] {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash)
  return [
    parseRoute(hash),
    (route) => {
      window.location.hash = routeHash(route)
    },
  ]
}
