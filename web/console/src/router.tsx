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
import { ActorPage } from "@/pages/actor"
import { AgentChatPage } from "@/pages/agent-chat"
import { AgentsPage } from "@/pages/agents"
import { BundleDetailPage } from "@/pages/bundle-detail"
import { AuthorityPage, PackagePage } from "@/pages/authority"
import { ChangeRequestDetailPage } from "@/pages/change-request-detail"
import { ConnectionDetailPage } from "@/pages/connection-detail"
import { ConnectionsPage } from "@/pages/connections"
import { HomePage } from "@/pages/home"
import { LoginPage } from "@/pages/login"
import { MergeRequestDetailPage } from "@/pages/merge-request-detail"
import { RecordPage } from "@/pages/record"
import { RecordEditPage, RecordNewPage } from "@/pages/record-editor"
import { RegisterPage } from "@/pages/register"
import { RegistryPage } from "@/pages/registry"
import { SearchPage } from "@/pages/search"
import { BundleSettingsPage } from "@/pages/settings"
import { KindBrowsePage } from "@/pages/kind-browse"
import { AllDataPage } from "@/pages/all-data"
import { HistoryPage } from "@/pages/history"
import { ToolsPage } from "@/pages/tools"
import { ToolPage } from "@/pages/tool"
import { ProvidersPage } from "@/pages/providers"
import { ProviderPage } from "@/pages/provider"
import { ConsoleSettingsPage } from "@/pages/console-settings"

const rootRoute = createRootRoute()

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

// The changelog is History now; an old link lands there with its facets.
export const changelogRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/changelog",
  beforeLoad: ({ location }) => {
    throw redirect({ to: "/history", search: location.search, replace: true })
  },
})

export const registryRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/registry",
  component: RegistryPage,
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

export const bundleDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/registry/$id",
  component: BundleDetailPage,
})

export const connectionsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/connections",
  component: ConnectionsPage,
})

// One Connection is one account record, so its address is the record's own
// kind reference plus the id, the way a data address is (decision 0047).
export const connectionDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/connections/$authority/$pkg/$name/$id",
  component: ConnectionDetailPage,
})

export const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: ConsoleSettingsPage,
})

// The settings list is the index; one bundle's form is the page under it, and
// the `$id` is the bundle id (`<authority>/<package>`), the same prefix its
// setting records carry.
export const bundleSettingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings/$id",
  component: BundleSettingsPage,
})

export const agentsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/agents",
  component: AgentsPage,
})

export const agentChatRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/agents/$id",
  component: AgentChatPage,
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

// The account and its tokens live in Settings now. NOT "/tokens" for either:
// the API door answers `GET /tokens`, so a browser loading that path would get
// JSON, not the SPA.
export const tokensRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/account/tokens",
  beforeLoad: () => {
    throw redirect({ to: "/settings", replace: true })
  },
})

export const accountRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/account",
  beforeLoad: () => {
    throw redirect({ to: "/settings", replace: true })
  },
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
export const providerRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/providers/$authority/$pkg",
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
