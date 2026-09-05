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
import { AgentChatPage } from "@/pages/agent-chat"
import { AgentsPage } from "@/pages/agents"
import { ChangelogPage } from "@/pages/changelog"
import { BundleDetailPage } from "@/pages/bundle-detail"
import { AuthorityPage, PackagePage } from "@/pages/authority"
import { ChangeRequestDetailPage } from "@/pages/change-request-detail"
import { HomePage } from "@/pages/home"
import { LoginPage } from "@/pages/login"
import { MergeRequestDetailPage } from "@/pages/merge-request-detail"
import { RecordPage } from "@/pages/record"
import { RecordEditPage, RecordNewPage } from "@/pages/record-editor"
import { RegisterPage } from "@/pages/register"
import { RegistryPage } from "@/pages/registry"
import { TokensPage } from "@/pages/tokens"
import { KindBrowsePage } from "@/pages/kind-browse"

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

export const changelogRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/changelog",
  component: ChangelogPage,
})

export const registryRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/registry",
  component: RegistryPage,
})

export const bundleDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/registry/$id",
  component: BundleDetailPage,
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

const routeTree = rootRoute.addChildren([
  loginRoute,
  registerRoute,
  shellRoute.addChildren([
    homeRoute,
    changelogRoute,
    registryRoute,
    bundleDetailRoute,
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
