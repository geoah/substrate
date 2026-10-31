/** An app's approval page (`/authorize/$request`, decision 0194). An app sent
 * the owner here through `GET /oauth/authorize`; the server parked its request
 * under the id in the path. The page shows who is asking, where the code goes,
 * the repository the token opens and each scope with the kinds or the agents
 * it reaches today (decision 0195), and the owner approves some of the scopes
 * for one lifetime or denies the lot.
 *
 * Either answer carries the URL to go to (the app's redirect URI with the code
 * or with the refusal), and the page navigates there with
 * `window.location.assign`: the console's CSP has `form-action 'self'`, so a
 * form post that redirects off the origin would be blocked. An error never
 * navigates anywhere. */

import { useId, useState, type ReactNode } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { BoxesIcon, TriangleAlertIcon } from "lucide-react"

import { ShellServer } from "@/components/shell-server"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Segmented } from "@/components/ui/segmented"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { useDocumentTitle } from "@/hooks/use-document-title"
import {
  approveAuthorization,
  denyAuthorization,
  getAuthorization,
} from "@/lib/api/authorize"
import {
  ApiError,
  type AuthorizationAnswer,
  type AuthorizationApproval,
  type AuthorizationKind,
  type AuthorizationRequest,
  type AuthorizationScope,
  type SubstrateRecord,
} from "@/lib/api/types"
import { agentName, canChange, canSee, joinWords } from "@/lib/agent-chat"
import { overAgents } from "@/lib/app-scopes"
import { relativeTime, shortDateTime } from "@/lib/format"
import { authorizeRoute } from "@/router"

/** How many kinds a scope lists before it offers the rest. */
const KIND_PREVIEW = 8

type Verdict = "approve" | "deny"

/** The page's frame: the doors' centred column, wider, since full URLs and
 * kind references wrap rather than truncate. */
function Door({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-6">
      <div className="flex w-full max-w-xl flex-col gap-6">
        <div className="flex items-center justify-center gap-2.5">
          <div className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <BoxesIcon className="size-4" />
          </div>
          <span className="text-lg font-semibold">substrate</span>
        </div>
        {children}
        <ShellServer />
      </div>
    </div>
  )
}

/** One labelled fact on the page: a small heading over its value. */
function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section aria-label={label} className="flex flex-col gap-1.5">
      <h2 className="text-xs font-medium text-muted-foreground">{label}</h2>
      {children}
    </section>
  )
}

function Warning({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <div
      role="note"
      className="flex gap-2.5 rounded-lg border border-warning/40 bg-warn-soft px-3 py-2.5 text-[13px]"
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-warning"
      />
      <div className="flex min-w-0 flex-col gap-0.5">
        {title && <p className="font-medium">{title}</p>}
        <p>{children}</p>
      </div>
    </div>
  )
}

/** What an error means here. A 404 is a request the server no longer holds:
 * it lapsed, or somebody already answered it. */
function refusalWords(
  error: unknown,
  verdict: Verdict | "read"
): { title: string; detail: string } {
  if (error instanceof ApiError && error.status === 404) {
    return {
      title: "This request has expired or was already answered.",
      detail: "Go back to the app and start again from there.",
    }
  }
  return {
    title:
      verdict === "deny"
        ? "This request couldn’t be denied."
        : "This request can’t be approved.",
    detail: error instanceof Error ? error.message : "Something went wrong.",
  }
}

function Refusal({
  error,
  verdict,
}: {
  error: unknown
  verdict: Verdict | "read"
}) {
  const words = refusalWords(error, verdict)
  return (
    <div
      role="alert"
      className="flex gap-2.5 rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2.5 text-[13px]"
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-destructive"
      />
      <div className="flex min-w-0 flex-col gap-0.5">
        <p className="font-medium text-destructive">{words.title}</p>
        <p className="[overflow-wrap:anywhere] text-muted-foreground">
          {words.detail}
        </p>
      </div>
    </div>
  )
}

/** A lifetime as the owner picks it; 0 is a token with no expiry. */
function lifetimeWords(days: number): string {
  if (days === 0) return "Until you revoke it"
  return days === 1 ? "1 day" : `${days} days`
}

/** Only a web address is followed: the answer is the app's redirect URI, and
 * anything else (a `javascript:` URL) is not somewhere this page may send the
 * owner. */
function isWebAddress(value: string): boolean {
  try {
    const url = new URL(value)
    return url.protocol === "https:" || url.protocol === "http:"
  } catch {
    return false
  }
}

function KindList({ kinds }: { kinds: AuthorizationKind[] }) {
  const [all, setAll] = useState(false)
  if (kinds.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        Nothing yet: no kind in your repository matches.
      </p>
    )
  }
  const shown = all ? kinds : kinds.slice(0, KIND_PREVIEW)
  return (
    <div className="flex flex-col items-start gap-1">
      <ul className="flex w-full flex-col gap-1">
        {shown.map((k) => (
          <li
            key={k.identity}
            className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5"
          >
            {k.label && <span className="text-[13px]">{k.label.plural}</span>}
            <span className="min-w-0 font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
              {k.identity}
            </span>
          </li>
        ))}
      </ul>
      {kinds.length > KIND_PREVIEW && (
        <Button
          variant="link"
          size="xs"
          className="px-0"
          onClick={() => setAll((v) => !v)}
        >
          {all ? "Show fewer" : `Show all ${kinds.length} kinds`}
        </Button>
      )}
    </div>
  )
}

/** The agents an agents or decide scope matches today. Talking to an agent
 * reaches whatever it may do, so each says what it can see and change in the
 * chat surface's words. */
function AgentList({
  agents,
  talk,
}: {
  agents: SubstrateRecord[]
  talk: boolean
}) {
  if (agents.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        Nothing yet: no agent in your repository matches.
      </p>
    )
  }
  return (
    <ul className="flex w-full flex-col gap-2">
      {agents.map((a) => (
        <li key={a.id} className="flex flex-col gap-0.5">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="text-[13px]">{agentName(a.id)}</span>
            <span className="min-w-0 font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
              {a.id}
            </span>
          </div>
          {talk && (
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
              <dt className="text-muted-foreground">Can see</dt>
              <dd>{canSee(a) ?? "Nothing"}</dd>
              <dt className="text-muted-foreground">Can change</dt>
              <dd>{canChange(a)}</dd>
            </dl>
          )}
        </li>
      ))}
    </ul>
  )
}

/** An agents or decide row's heading: what the app may do, with the agents
 * it matches by name. */
function agentsHeading(scope: AuthorizationScope): string {
  const names = joinWords(scope.agents.map((a) => agentName(a.id))) || "agents"
  if (scope.verb === "agents") return `Talk to ${names} and read your chats`
  const verb = scope.agents.length === 1 ? "suggests" : "suggest"
  return `Apply, edit or dismiss changes ${names} ${verb}, with your authority`
}

function ScopeRow({
  scope,
  repository,
  checked,
  disabled,
  onChange,
}: {
  scope: AuthorizationScope
  repository: string
  checked: boolean
  disabled: boolean
  onChange: (checked: boolean) => void
}) {
  const id = useId()
  const agentsScope = overAgents(scope.verb)
  return (
    <li className="flex gap-3 px-3 py-3">
      <input
        id={id}
        type="checkbox"
        className="mt-0.5 size-4 shrink-0 cursor-pointer accent-primary disabled:cursor-default"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        {agentsScope ? (
          <>
            <label htmlFor={id} className="cursor-pointer font-medium">
              {agentsHeading(scope)}
            </label>
            <p className="text-xs text-muted-foreground">
              Agents matching{" "}
              <span className="font-mono [overflow-wrap:anywhere] text-foreground">
                {scope.pattern}
              </span>
            </p>
          </>
        ) : (
          <label htmlFor={id} className="cursor-pointer font-medium">
            {scope.write ? "Read and write records of " : "Read records of "}
            <span className="font-mono text-[13px] [overflow-wrap:anywhere]">
              {scope.pattern}
            </span>
          </label>
        )}
        {scope.glob && (
          <p className="text-xs text-muted-foreground">
            {agentsScope
              ? "Also covers matching agents you add later."
              : "Also covers matching kinds you add later."}
          </p>
        )}
        {scope.verb === "agents" && (
          <p className="text-xs text-muted-foreground">
            The app reads every chat with these agents, yours included, answers
            their questions, and may rename or stop a chat. A chat reaches
            whatever these agents may do. Its messages are the app’s; the agents
            work with their own permissions.
          </p>
        )}
        {scope.verb === "decide" && (
          <p className="text-xs text-muted-foreground">
            The app may edit a change before it applies it. A change the app
            accepts is applied as if you accepted it, and your history shows the
            decision as the app’s.
          </p>
        )}
        {scope.requested !== scope.scope && (
          <p className="text-xs text-muted-foreground">
            Asked for as{" "}
            <span className="font-mono [overflow-wrap:anywhere] text-foreground">
              {scope.requested}
            </span>
            , where <span className="font-mono text-foreground">~</span> means
            your repository,{" "}
            <span className="font-mono [overflow-wrap:anywhere] text-foreground">
              {repository}
            </span>
            .
          </p>
        )}
        <div className="flex flex-col gap-1">
          <p className="text-xs font-medium text-muted-foreground">
            Covers today
          </p>
          {agentsScope ? (
            <AgentList agents={scope.agents} talk={scope.verb === "agents"} />
          ) : (
            <KindList kinds={scope.kinds} />
          )}
        </div>
      </div>
    </li>
  )
}

function Approval({
  id,
  request,
}: {
  id: string
  request: AuthorizationRequest
}) {
  const { app } = request
  // Every scope starts checked; the set holds the ones the owner unchecked.
  const [unchecked, setUnchecked] = useState<ReadonlySet<string>>(
    () => new Set()
  )
  const [lifetime, setLifetime] = useState(request.defaultLifetimeDays)
  const chosen = request.scopes
    .filter((s) => !unchecked.has(s.scope))
    .map((s) => s.scope)

  const answer = useMutation({
    mutationFn: async (input: {
      verdict: Verdict
      approval?: AuthorizationApproval
    }): Promise<AuthorizationAnswer> => {
      const reply =
        input.verdict === "approve" && input.approval
          ? await approveAuthorization(id, input.approval)
          : await denyAuthorization(id)
      if (!isWebAddress(reply.redirect)) {
        throw new Error(
          "The server answered with an address that is not a web page, so the console did not follow it."
        )
      }
      return reply
    },
    onSuccess: (reply) => {
      window.location.assign(reply.redirect)
    },
  })

  const gone = answer.error instanceof ApiError && answer.error.status === 404
  // After an answer the page is on its way out; nothing may be sent twice.
  const settled = answer.isPending || answer.isSuccess || gone
  const pendingVerdict = answer.isPending ? answer.variables.verdict : undefined
  // Talking to an agent writes the app's messages, and deciding writes the
  // decision, both under the app's name.
  const writes = request.scopes.some((s) => s.verb !== "read")

  function toggle(scope: string, checked: boolean) {
    setUnchecked((prev) => {
      const next = new Set(prev)
      if (checked) next.delete(scope)
      else next.add(scope)
      return next
    })
  }

  return (
    <Card>
      <CardHeader className="gap-1.5">
        <p className="text-xs text-muted-foreground">
          An app is asking for access
        </p>
        <h1 className="font-heading text-xl leading-snug font-semibold [overflow-wrap:anywhere]">
          {app.host}
        </h1>
        <p className="text-xs text-muted-foreground">
          <span>Client id </span>
          <span className="font-mono [overflow-wrap:anywhere]">
            {app.clientId}
          </span>
        </p>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {app.local && (
          <Warning title="This app says it runs on this computer">
            Nothing about it can be verified, not even its name or where it
            runs. Approve only if you started it yourself.
          </Warning>
        )}

        {(app.name || app.uri) && (
          <Fact label="The app says it is">
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1">
              {app.name && (
                <>
                  <dt className="text-muted-foreground">Name</dt>
                  <dd className="[overflow-wrap:anywhere]">{app.name}</dd>
                </>
              )}
              {app.uri && (
                <>
                  <dt className="text-muted-foreground">Website</dt>
                  <dd className="font-mono text-xs leading-5 [overflow-wrap:anywhere]">
                    {app.uri}
                  </dd>
                </>
              )}
            </dl>
            <p className="text-xs text-faint">
              The app wrote this about itself. Nothing checks it.
            </p>
            {app.uriHostDiffers && (
              <Warning>
                The website it names is not on {app.host}. Its client id does
                not prove that it runs that site.
              </Warning>
            )}
          </Fact>
        )}

        <Fact label="After you answer, you go to">
          <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
            {request.redirect.uri}
          </p>
          {request.redirect.loopback && !app.local && (
            <Warning title="A program on this computer receives the access">
              Nothing confirms that program is {app.host}. Approve only if you
              started the app yourself.
            </Warning>
          )}
        </Fact>

        <Fact label="Repository">
          <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
            {request.repository}
          </p>
        </Fact>

        <Fact label="Requested access">
          <p className="text-xs text-muted-foreground">
            Uncheck anything you don’t want to allow.
          </p>
          <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
            {request.scopes.map((s) => (
              <ScopeRow
                key={s.scope}
                scope={s}
                repository={request.repository}
                checked={!unchecked.has(s.scope)}
                disabled={settled}
                onChange={(checked) => toggle(s.scope, checked)}
              />
            ))}
          </ul>
        </Fact>

        {writes && (
          <Fact label="Its changes appear as">
            <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
              {app.actor}
            </p>
            <p className="text-xs text-muted-foreground">
              Your history shows what this app changed under this name, never as
              yours.
            </p>
            <Warning title="Your automations run on its changes">
              Your triggers and mappings respond to its changes like any other.
              They may run code you installed, call services that charge you, or
              change records this app cannot reach itself.
            </Warning>
          </Fact>
        )}

        <Fact label="Access lasts">
          <Segmented
            label="Access lasts"
            value={String(lifetime)}
            options={request.lifetimeDays.map((d) => ({
              value: String(d),
              label: lifetimeWords(d),
            }))}
            onChange={(v) => setLifetime(Number(v))}
            disabled={settled}
            className="self-start"
          />
          <p className="text-xs text-muted-foreground">
            {lifetime === 0
              ? "It lasts until you revoke it in Settings, under Apps."
              : "After that, the app has to ask you again. You can revoke it sooner in Settings, under Apps."}
          </p>
        </Fact>

        <Separator />

        {answer.isError && (
          <Refusal error={answer.error} verdict={answer.variables.verdict} />
        )}
        {answer.isSuccess && (
          <p role="status" className="text-sm text-muted-foreground">
            Taking you back to the app…
          </p>
        )}
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button
              variant="outline"
              disabled={settled}
              onClick={() => answer.mutate({ verdict: "deny" })}
            >
              {pendingVerdict === "deny" && <Spinner />}
              Deny
            </Button>
            <Button
              disabled={settled || chosen.length === 0}
              onClick={() =>
                answer.mutate({
                  verdict: "approve",
                  approval: { scopes: chosen, lifetimeDays: lifetime },
                })
              }
            >
              {pendingVerdict === "approve" && <Spinner />}
              Approve
            </Button>
          </div>
          {chosen.length === 0 && !settled && (
            <p className="text-right text-xs text-muted-foreground">
              Check at least one, or deny the request.
            </p>
          )}
          <p
            className="text-right text-xs text-faint"
            title={shortDateTime(request.expiresAt)}
          >
            This request lapses {relativeTime(request.expiresAt)} if you do not
            answer it.
          </p>
        </div>
      </CardContent>
    </Card>
  )
}

export function AuthorizePage() {
  useDocumentTitle("Authorize an app")
  const { request: id } = authorizeRoute.useParams()
  // Read once: the request is a snapshot, and a refetch after an answer would
  // only find it gone. A dropped connection or a busy server is still
  // retried, the console's default for a read; a 404 never is.
  const authorization = useQuery({
    queryKey: ["authorization", id],
    queryFn: ({ signal }) => getAuthorization(id, signal),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })

  return (
    <Door>
      {authorization.isPending ? (
        <Card aria-busy>
          <CardHeader>
            <CardTitle>Authorize an app</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <span className="sr-only">Reading the request</span>
            <Skeleton className="h-6 w-1/2" />
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-24 w-full" />
          </CardContent>
        </Card>
      ) : authorization.isError ? (
        <Card>
          <CardHeader>
            <CardTitle>Authorize an app</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Refusal error={authorization.error} verdict="read" />
            {!(
              authorization.error instanceof ApiError &&
              authorization.error.status < 500
            ) && (
              <div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void authorization.refetch()}
                >
                  Try again
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      ) : (
        <Approval id={id} request={authorization.data} />
      )}
    </Door>
  )
}
