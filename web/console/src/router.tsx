/** Code-based route tree. Everything except /login sits under the shell
 * route, whose beforeLoad is the session gate: no token, no console. */

import {
  createRootRoute,
  createRoute,
  createRouter,
  redirect,
} from "@tanstack/react-router"

import { AppShell } from "@/components/app-shell"
import { hasSession } from "@/lib/api/session"
import { AccountPage } from "@/pages/account"
import { ActorPage } from "@/pages/actor"
import { AgentsPage } from "@/pages/agents"
import { ChangelogPage } from "@/pages/changelog"
import { AuthorityPage, PackagePage } from "@/pages/authority"
import { ChangeRequestDetailPage } from "@/pages/change-request-detail"
import { HomePage } from "@/pages/home"
import { LoginPage } from "@/pages/login"
import { MergeRequestDetailPage } from "@/pages/merge-request-detail"
import { RecordPage } from "@/pages/record"
import { RecordEditPage, RecordNewPage } from "@/pages/record-editor"
import { RegisterPage } from "@/pages/register"
import { SearchPage } from "@/pages/search"
import { TokensPage } from "@/pages/tokens"
import { KindBrowsePage } from "@/pages/kind-browse"
import { AllDataPage } from "@/pages/all-data"
import { HistoryPage } from "@/pages/history"
import { ToolsPage } from "@/pages/tools"
import { ToolPage } from "@/pages/tool"
import { ProvidersPage } from "@/pages/providers"
import { ProviderPage } from "@/pages/provider"
import { ConsoleSettingsPage } from "@/pages/console-settings"

const rootRoute = createRootRoute()

/** The authority the shipped samples are published under: an old Registry
 * address naming one is a sample to add from All data. */
const SAMPLES_AUTHORITY = "samples.substrate.reamde.dev"

/** A bundle id, `<authority>/<package>`, as the provider route's params. */
function bundleTarget(id: string): { authority: string; pkg: string } | null {
  const at = id.lastIndexOf("/")
  if (at <= 0 || at === id.length - 1) return null
  return { authority: id.slice(0, at), pkg: id.slice(at + 1) }
}

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect:
      typeof search.redirect === "string" && search.redirect
        ? search.redirect
        : undefined,
  }),
  beforeLoad: () => {
    // A live session has no business on the login page.
    if (hasSession()) throw redirect({ to: "/" })
  },
  component: LoginPage,
})

export const registerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/register",
  beforeLoad: () => {
    // A live session has no business on the registration page.
    if (hasSession()) throw redirect({ to: "/" })
  },
  component: RegisterPage,
})

const shellRoute = createRoute({
  id: "shell",
  getParentRoute: () => rootRoute,
  beforeLoad: ({ location }) => {
    if (!hasSession()) {
      throw redirect({
        to: "/login",
        search: { redirect: location.href === "/" ? undefined : location.href },
      })
    }
  },
  component: AppShell,
})

export const homeRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: HomePage,
})

export const changelogRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/changelog",
  component: ChangelogPage,
})

// The Registry, Connections and bundle Settings pages became Providers; their
// old addresses still land somewhere true.
export const registryRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/registry",
  beforeLoad: () => {
    throw redirect({ to: "/providers" })
  },
})

// The ranked read as a page. The page reads its state through nuqs; the
// route declares the same three keys so a typed navigation (the ⌘K palette
// handing over a query) can spell them, and so nothing strips them.
export const searchRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/search",
  validateSearch: (
    search: Record<string, unknown>
  ): { q?: string; mode?: string; kind?: string } => ({
    q: typeof search.q === "string" && search.q ? search.q : undefined,
    mode:
      typeof search.mode === "string" && search.mode ? search.mode : undefined,
    kind:
      typeof search.kind === "string" && search.kind ? search.kind : undefined,
  }),
  component: SearchPage,
})

// A shipped sample is added from All data now; anything else the old page
// showed (a provider, a package this repository holds) has a Providers page.
export const bundleDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/registry/$id",
  beforeLoad: ({ params }) => {
    const target = bundleTarget(params.id)
    if (!target || target.authority === SAMPLES_AUTHORITY) {
      throw redirect({ to: "/data" })
    }
    throw redirect({ to: "/providers/$authority/$pkg", params: target })
  },
})

export const connectionsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/connections",
  beforeLoad: () => {
    throw redirect({ to: "/providers" })
  },
})

// An account's kind lives in its provider's package, so the kind's authority
// and package ARE the provider's bundle id.
export const connectionDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/connections/$authority/$pkg/$name/$id",
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/providers/$authority/$pkg",
      params: { authority: params.authority, pkg: params.pkg },
      search: { account: params.id },
    })
  },
})

export const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: ConsoleSettingsPage,
})

// A bundle's settings live on its Providers page; the `$id` is the bundle id
// (`<authority>/<package>`), the same prefix its setting records carry.
export const bundleSettingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings/$id",
  beforeLoad: ({ params }) => {
    const target = bundleTarget(params.id)
    if (!target) throw redirect({ to: "/providers" })
    throw redirect({
      to: "/providers/$authority/$pkg",
      params: target,
      hash: "settings",
    })
  },
})

export const agentsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/agents",
  component: AgentsPage,
})

// The old per-agent address: the chat app opens a new chat with that agent,
// and a `?thread=` it carried opens that conversation instead.
export const agentChatRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/agents/$id",
  beforeLoad: ({ params, search }) => {
    const thread = (search as Record<string, unknown>).thread
    throw redirect({
      to: "/agents",
      search: (typeof thread === "string" && thread
        ? { thread }
        : { agent: params.id }) as never,
      replace: true,
    })
  },
})

export const mergeRequestDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/merge-requests/$id",
  component: MergeRequestDetailPage,
})

// Sibling of the merge-request route, and named for what it reviews rather than
// for the kind: `recordpatchrequest` carries create and delete as well as
// patch, so "changes" is the honest word. NOT "/changes": the changelog owns
// that noun in this console.
export const changeRequestDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/change-requests/$id",
  component: ChangeRequestDetailPage,
})

export const authorityRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/data/$authority",
  component: AuthorityPage,
})

// A data address is the kind reference, segment for segment (decision 0047):
// authority, package, kind, then the record id.
export const packageRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/data/$authority/$pkg",
  component: PackagePage,
})

export const kindBrowseRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/data/$authority/$pkg/$name",
  component: KindBrowsePage,
})

// The static `new` segment sits beside `$id` and wins the match (a create has
// no record yet); `$id/edit` is the edit surface for an existing one.
export const recordNewRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/data/$authority/$pkg/$name/new",
  component: RecordNewPage,
})

export const recordRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/data/$authority/$pkg/$name/$id",
  component: RecordPage,
})

export const recordEditRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/data/$authority/$pkg/$name/$id/edit",
  component: RecordEditPage,
})

export const actorRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/actors/$actorId",
  component: ActorPage,
})

export const tokensRoute = createRoute({
  getParentRoute: () => shellRoute,
  // NOT "/tokens": the API door answers `GET /tokens`, so a browser loading or
  // refreshing that path would get JSON, not the SPA. The console route nests
  // under /account, which the door does not serve.
  path: "/account/tokens",
  component: TokensPage,
})

export const accountRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/account",
  component: AccountPage,
})

export const allDataRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/data",
  component: AllDataPage,
})

export const historyRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/history",
  component: HistoryPage,
})

export const toolsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/tools",
  component: ToolsPage,
})

// A tool is a function record, addressed by its reference segment for segment,
// the way a data address is (decision 0047).
export const toolRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/tools/$authority/$pkg/$name",
  component: ToolPage,
})

export const providersRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/providers",
  component: ProvidersPage,
})

// A provider is a bundle, and a bundle's id is its package: authority, package.
// `account` names one of its accounts to scroll to and highlight.
export const providerRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/providers/$authority/$pkg",
  validateSearch: (search: Record<string, unknown>): { account?: string } => ({
    account:
      typeof search.account === "string" && search.account
        ? search.account
        : undefined,
  }),
  component: ProviderPage,
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  registerRoute,
  shellRoute.addChildren([
    homeRoute,
    changelogRoute,
    registryRoute,
    searchRoute,
    bundleDetailRoute,
    connectionsRoute,
    connectionDetailRoute,
    settingsRoute,
    bundleSettingsRoute,
    agentsRoute,
    agentChatRoute,
    mergeRequestDetailRoute,
    changeRequestDetailRoute,
    authorityRoute,
    packageRoute,
    kindBrowseRoute,
    recordNewRoute,
    recordRoute,
    recordEditRoute,
    actorRoute,
    tokensRoute,
    accountRoute,
    allDataRoute,
    historyRoute,
    toolsRoute,
    toolRoute,
    providersRoute,
    providerRoute,
  ]),
])

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  // Record ids carry `@` (calendar/email-derived ids); leaving it raw in the
  // URL keeps the address bar honest to the id the API stores.
  pathParamsAllowedCharacters: ["@"],
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}
