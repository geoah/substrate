/** Code-based route tree. Everything except the doors (/login, /register),
 * an app's approval page (/authorize/$request) and a standalone applet (/a/$id)
 * sits under the shell route, whose beforeLoad is the session gate: no
 * token, no console. The approval page and a standalone applet keep a gate of
 * their own, outside the shell. */

import {
  createRootRoute,
  createRoute,
  createRouter,
  redirect,
} from "@tanstack/react-router"
import { NuqsAdapter } from "nuqs/adapters/tanstack-router"

import { PageError } from "@/components/page-error"
import { ShellOrLanding } from "@/components/site/shell-or-landing"
import { LOCAL_AUTHORITY } from "@/lib/api/http"
import { getRepository, hasSession } from "@/lib/api/session"
import { ActorPage } from "@/pages/actor"
import { AgentPage } from "@/pages/agent"
import { AgentsPage } from "@/pages/agents"
import { AuthorityPage, PackagePage } from "@/pages/authority"
import { AuthorizePage } from "@/pages/authorize"
import { ChangeRequestDetailPage } from "@/pages/change-request-detail"
import { HomePage } from "@/pages/home"
import { LoginPage } from "@/pages/login"
import { MergeRequestDetailPage } from "@/pages/merge-request-detail"
import { RecordPage } from "@/pages/record"
import { RecordEditPage, RecordNewPage } from "@/pages/record-editor"
import { RegisterPage } from "@/pages/register"
import { SearchPage } from "@/pages/search"
import { KindBrowsePage } from "@/pages/kind-browse"
import { AllDataPage } from "@/pages/all-data"
import { HistoryPage } from "@/pages/history"
import { ObservationsPage } from "@/pages/observations"
import { ObservationsTimelinePage } from "@/pages/observations-timeline"
import { ToolsPage } from "@/pages/tools"
import { ToolPage } from "@/pages/tool"
import { ProvidersPage } from "@/pages/providers"
import {
  oauthReturnRedirect,
  oauthReturnSearch,
  type OAuthReturnSearch,
} from "@/lib/oauth-return"
import { loginHint } from "@/lib/login-hint"
import { safeReturnPath } from "@/lib/return-path"
import { searchText } from "@/lib/search-params"
import { loadSite } from "@/lib/site"
import { ProviderPage } from "@/pages/provider"
import { ConsoleSettingsPage } from "@/pages/console-settings"
import { OwnerNamePage } from "@/pages/owner-name"
import { AssistantsPage } from "@/pages/assistants"
import { AppletPage, AppletsPage, StandaloneAppletPage } from "@/pages/applets"
import { appletSearch } from "@/lib/applet-location"

const rootRoute = createRootRoute()

/** The authority the shipped samples are published under: an old Registry
 * address naming one is a sample to add from All data. */
const SAMPLES_AUTHORITY = "samples.substrate.reamde.dev"

/** A bundle id, `<authority>/<package>`, as the provider route's params. A
 * package of the repository's own named by its full name is the one under
 * `~` (decision record 0259). */
function bundleTarget(id: string): { authority: string; pkg: string } | null {
  const at = id.lastIndexOf("/")
  if (at <= 0 || at === id.length - 1) return null
  const authority = id.slice(0, at)
  return {
    authority: authority === getRepository() ? LOCAL_AUTHORITY : authority,
    pkg: id.slice(at + 1),
  }
}

/** The addresses whose first segment after the section is an authority. */
const AUTHORITY_ADDRESS = /^\/(data|tools|providers)\/([^/?#]+)(.*)$/

/** `href` with the repository's full name in its authority segment moved to
 * `~`, or undefined where it names no such thing. The name is another
 * spelling of `~` (decision record 0259), and the registry lists the
 * repository's own kinds under `~` alone, so an older link under the name
 * would find nothing there. */
export function localHref(
  href: string,
  repository: string | null
): string | undefined {
  const m = AUTHORITY_ADDRESS.exec(href)
  if (!repository || !m || m[2] !== repository) return undefined
  return `/${m[1]}/${LOCAL_AUTHORITY}${m[3]}`
}

/** The doors' `?redirect=`: where to go once signed in. Kept as written here;
 * every reader passes it through `safeReturnPath` before following it.
 * `?login_hint=` is the repository an app asked for (decision 0208), kept
 * only when it is a valid repository name. */
function doorSearch(search: Record<string, unknown>): {
  redirect?: string
  login_hint?: string
} {
  return {
    redirect:
      typeof search.redirect === "string" && search.redirect
        ? search.redirect
        : undefined,
    login_hint: loginHint(search.login_hint),
  }
}

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: doorSearch,
  beforeLoad: ({ search }) => {
    // A live session has no business on the login page: it goes where the
    // sign-in would have sent it.
    if (hasSession()) {
      throw redirect({ to: safeReturnPath(search.redirect) ?? "/" })
    }
  },
  component: LoginPage,
})

export const registerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/register",
  validateSearch: doorSearch,
  beforeLoad: ({ search }) => {
    // A live session has no business on the registration page.
    if (hasSession()) {
      throw redirect({ to: safeReturnPath(search.redirect) ?? "/" })
    }
  },
  component: RegisterPage,
})

// An app's approval page (decision 0194). The server parks the app's request
// and sends the browser here with only its id in the path, so the router never
// parses an OAuth parameter. It renders outside the shell, like the doors, and
// a signed-out owner goes through /login and comes back by its plain path.
export const authorizeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/authorize/$request",
  // The server adds `?login_hint=` when the app named a repository, and the
  // sign-in fills its field with it.
  validateSearch: (search: Record<string, unknown>) => ({
    login_hint: loginHint(search.login_hint),
  }),
  beforeLoad: ({ params, search }) => {
    if (!hasSession()) {
      throw redirect({
        to: "/login",
        search: {
          redirect: `/authorize/${encodeURIComponent(params.request)}`,
          login_hint: search.login_hint,
        },
      })
    }
  },
  component: AuthorizePage,
})

// An applet on its own (docs/plans/applets.md): the whole viewport, none of
// the shell, so a phone can keep it on its home screen. It keeps a gate of
// its own, like the approval page, and a signed-out owner comes back to the
// same screen.
export const standaloneAppletRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/a/$id",
  validateSearch: appletSearch,
  beforeLoad: ({ location }) => {
    if (!hasSession()) {
      throw redirect({ to: "/login", search: { redirect: location.href } })
    }
  },
  component: StandaloneAppletPage,
})

/** The shell's gate. A signed-out reader goes to the sign-in, except at `/`
 * on a deployment that sets a landing page, where the page renders in the
 * shell's place. The site is only asked for there, so a signed-in console
 * and a deployment without one never wait on it. */
export async function shellGate(location: {
  pathname: string
  href: string
}): Promise<void> {
  if (hasSession()) return
  if (location.pathname === "/" && (await loadSite())?.landing) return
  throw redirect({
    to: "/login",
    search: { redirect: location.href === "/" ? undefined : location.href },
  })
}

const shellRoute = createRoute({
  id: "shell",
  getParentRoute: () => rootRoute,
  beforeLoad: ({ location }) => {
    if (!hasSession()) return shellGate(location)
    const local = localHref(location.href, getRepository())
    if (local) throw redirect({ href: local, replace: true })
    return undefined
  },
  component: ShellOrLanding,
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

// The Registry, Connections and bundle Settings pages became Providers; their
// old addresses still land somewhere true.
// It is also where the substrate's OAuth return page falls back to
// (`?connected=` / `?error=`), so the result travels on with the redirect.
export const registryRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/registry",
  beforeLoad: ({ location }) => {
    throw redirect({
      ...oauthReturnRedirect(location.search as Record<string, unknown>),
      replace: true,
    })
  },
})

// The ranked read as a page. The page reads its state through nuqs; the
// route declares the same three keys so a typed navigation (the ⌘K palette
// handing over a query) can spell them, and so nothing strips them. A search
// for `2024` arrives parsed as a number, so each key is read as text.
export const searchRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/search",
  validateSearch: (
    search: Record<string, unknown>
  ): { q?: string; mode?: string; kind?: string } => ({
    q: searchText(search.q),
    mode: searchText(search.mode),
    kind: searchText(search.kind),
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

// The AI assistants page (decision 0230). A static segment outranks the
// bundle redirect's `$id` below, so `assistants` never reads as a bundle.
export const assistantsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings/assistants",
  component: AssistantsPage,
})

// The owner's name, for a repository registered before registration asked
// it. A static segment, like `assistants`, outranks the bundle redirect.
export const ownerNameRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings/name",
  component: OwnerNamePage,
})

// Services was a page of its own before Providers listed the AI models and
// the operator's keys (decision 0233); a bookmark lands on Providers.
export const servicesRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/services",
  beforeLoad: () => {
    throw redirect({ to: "/providers", replace: true })
  },
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

// The chat app. The page reads these four keys through nuqs (pages/agents.tsx);
// the route declares them so a link into a chat is type-checked. Each is read
// as text: `?send=1` and a digit-only id arrive parsed as numbers, and a
// strings-only validator would strip them from the address.
export const agentsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/agents",
  validateSearch: (
    search: Record<string, unknown>
  ): {
    thread?: string
    agent?: string
    prompt?: string
    send?: string
    about?: string
  } => ({
    thread: searchText(search.thread),
    agent: searchText(search.agent),
    prompt: searchText(search.prompt),
    send: searchText(search.send),
    // A record path the next message is about ("Ask about this").
    about: searchText(search.about),
  }),
  component: AgentsPage,
})

// One agent's page. The old per-agent chat address carried `?thread=`, and
// that still opens the conversation in the chat app.
export const agentRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/agents/$id",
  beforeLoad: ({ search }) => {
    const thread = searchText((search as Record<string, unknown>).thread)
    if (thread) {
      throw redirect({ to: "/agents", search: { thread }, replace: true })
    }
  },
  component: AgentPage,
})

export const appletsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/applets",
  component: AppletsPage,
})

// One applet in the shell. The address holds its screen and that screen's
// params (`?screen=book&p.id=…`), so Back walks the screens.
export const appletRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/applets/$id",
  validateSearch: appletSearch,
  component: AppletPage,
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

// Measured values across every kind binding core's `observed` trait: the
// streams, and the timeline. Both read their filters through nuqs, so the
// routes declare no search params and strip none.
export const observationsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/observations",
  component: ObservationsPage,
})

export const observationsTimelineRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/observations/timeline",
  component: ObservationsTimelinePage,
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
  // `model` names an AI model card (an llm/provider row id) to scroll to,
  // with its key form open where it has no key: an agent's "Add your key"
  // links there.
  validateSearch: (
    search: Record<string, unknown>
  ): OAuthReturnSearch & { model?: string } => ({
    model: searchText(search.model),
    ...oauthReturnSearch(search),
  }),
  component: ProvidersPage,
})

// A provider is a bundle, and a bundle's id is its package: authority, package.
// `account` names one of its accounts to scroll to and highlight.
export const providerRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/providers/$authority/$pkg",
  validateSearch: (
    search: Record<string, unknown>
  ): OAuthReturnSearch & { account?: string } => ({
    account: searchText(search.account),
    ...oauthReturnSearch(search),
  }),
  component: ProviderPage,
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  registerRoute,
  authorizeRoute,
  standaloneAppletRoute,
  shellRoute.addChildren([
    homeRoute,
    changelogRoute,
    registryRoute,
    searchRoute,
    bundleDetailRoute,
    connectionsRoute,
    connectionDetailRoute,
    settingsRoute,
    assistantsRoute,
    ownerNameRoute,
    servicesRoute,
    bundleSettingsRoute,
    agentsRoute,
    agentRoute,
    appletsRoute,
    appletRoute,
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
    observationsRoute,
    observationsTimelineRoute,
    toolsRoute,
    toolRoute,
    providersRoute,
    providerRoute,
  ]),
])

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  // Every route catches its own render failure, so a page that fails keeps
  // the shell and its sidebar around it.
  defaultErrorComponent: PageError,
  // Record ids carry `@` (calendar/email-derived ids); leaving it raw in the
  // URL keeps the address bar honest to the id the API stores.
  pathParamsAllowedCharacters: ["@"],
  // nuqs's TanStack Router adapter reads the router from context (since
  // nuqs 2.10 its history spy calls useRouter), so it has to render inside
  // the router. Mounted around RouterProvider it got a null router and the
  // console rendered a blank page.
  InnerWrap: ({ children }) => <NuqsAdapter>{children}</NuqsAdapter>,
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}
