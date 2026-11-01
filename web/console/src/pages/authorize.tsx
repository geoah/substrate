/** An app's approval page (`/authorize/$request`, decision 0194). An app sent
 * the owner here through `GET /oauth/authorize`; the server parked its request
 * under the id in the path. The page leads with what the app will be able to
 * do and what approving sets up on the server, in plain words
 * (docs/plans/app-setup.md), and the console's one Technical details switch
 * (decision 0131) adds every identifier behind them. The owner approves some
 * of the scopes for one lifetime, or denies the lot.
 *
 * A denial, and an approval with nothing to set up, carry the URL to go to
 * (the app's redirect URI with the code or with the refusal). An approval
 * that sets something up answers `202`, and the page reads the request every
 * second until it is approved and carries `next`, or stopped with a failure.
 * The page navigates with `window.location.assign`: the console's CSP has
 * `form-action 'self'`, so a form post that redirects off the origin would
 * be blocked. An error never navigates anywhere. */

import { useEffect, useState, type ReactNode } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { BoxesIcon, ChevronRightIcon } from "lucide-react"

import { Fact, Problem, Warning } from "@/components/authorize/parts"
import { ScopeRow } from "@/components/authorize/scope-row"
import { SetupRow } from "@/components/authorize/setup-row"
import { ConsolePreferencesProvider } from "@/components/console-preferences"
import { CopyButton } from "@/components/identity/copy-button"
import { ToggleSwitch } from "@/components/nav/toggle-switch"
import { ShellServer } from "@/components/shell-server"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Segmented } from "@/components/ui/segmented"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { useDocumentTitle } from "@/hooks/use-document-title"
import {
  approveAuthorization,
  denyAuthorization,
  getAuthorization,
  isSettingUp,
  SETUP_POLL_MS,
} from "@/lib/api/authorize"
import {
  ApiError,
  type AuthorizationApproval,
  type AuthorizationPrevious,
  type AuthorizationRequest,
  type AuthorizationSetup,
} from "@/lib/api/types"
import {
  asSentence,
  blockedWords,
  dayWords,
  failureWords,
  foldRequest,
  lifetimeEnd,
  planChanges,
  progressWords,
  reauthorizationHeading,
  webhookTriggers,
  type SetupLine,
} from "@/lib/app-setup"
import { relativeTime, shortDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { authorizeRoute } from "@/router"

type Verdict = "approve" | "deny"

/** What an answer led to: somewhere to go, a set up the page now follows,
 * or a plan that moved under the page and wants another look. */
type Outcome =
  | { kind: "go"; to: string }
  | { kind: "setting-up"; rows?: AuthorizationSetup[] }
  | {
      kind: "moved"
      before: AuthorizationSetup[]
      fresh: AuthorizationRequest
    }

/** The page's frame: the doors' centred column, wider, since full URLs and
 * kind references wrap rather than truncate, and wider again on a desktop
 * for the technical view's identifiers. */
function Door({ wide, children }: { wide: boolean; children: ReactNode }) {
  return (
    <div className="flex min-h-svh w-full items-center justify-center bg-background p-6">
      <div
        className={cn(
          "flex w-full max-w-xl flex-col gap-6",
          wide && "lg:max-w-3xl"
        )}
      >
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

function isGone(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404
}

/** What an error means here. A 404 is a request the server no longer holds:
 * it lapsed, or somebody already answered it. */
function refusalWords(
  error: unknown,
  verdict: Verdict | "read"
): { title: string; detail: string } {
  if (isGone(error)) {
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
  return <Problem title={words.title}>{words.detail}</Problem>
}

/** A lifetime as the owner picks it; 0 is a token with no expiry. */
function lifetimeWords(days: number): string {
  if (days === 0) return "Until you revoke it"
  return days === 1 ? "1 day" : `${days} days`
}

/** Only a web address is followed: the answer is the app's redirect URI, and
 * anything else (a `javascript:` URL) is not somewhere this page may send the
 * owner. */
function isWebAddress(value: string | undefined): value is string {
  if (!value) return false
  try {
    const url = new URL(value)
    return url.protocol === "https:" || url.protocol === "http:"
  } catch {
    return false
  }
}

const SETTING_UP =
  "Setting up on your server. Closing this page does not stop it."

const CONTINUES =
  "Setting up continues even if you close this page. What it sets up stays if it stops part way or you later revoke the app."

const NOT_A_WEB_PAGE =
  "The server answered with an address that is not a web page, so the console did not follow it."

function followable(value: string | undefined): string {
  if (!isWebAddress(value)) throw new Error(NOT_A_WEB_PAGE)
  return value
}

/** The one warning the everyday view gives about where the access goes: a
 * local app nothing can verify, or a program on this device receiving it. */
function AccessWarning({ request }: { request: AuthorizationRequest }) {
  const { app, redirect } = request
  if (app.local) {
    return (
      <Warning title="This app says it runs on this device">
        Nothing about it can be verified, not even its name or where it runs.
        Approve only if you started it yourself.
      </Warning>
    )
  }
  if (redirect.loopback) {
    return (
      <Warning title="A program on this device receives the access">
        Nothing confirms that program is {app.host}. Approve only if you started
        the app yourself.
      </Warning>
    )
  }
  return null
}

/** Everything the page showed before it had an everyday view: who claims
 * what, where the code goes and the repository, each with its warning, and
 * the access the app holds, or held until it ended; the header says when it
 * was approved. */
function AppFacts({
  request,
  live,
}: {
  request: AuthorizationRequest
  live: boolean
}) {
  const { app } = request
  return (
    <>
      {app.local && (
        <Warning title="This app says it runs on this device">
          Nothing about it can be verified, not even its name or where it runs.
          Approve only if you started it yourself.
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
              The website it names is not on {app.host}. Its client id does not
              prove that it runs that site.
            </Warning>
          )}
        </Fact>
      )}

      <Fact label="After you answer, you go to">
        <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
          {request.redirect.uri}
        </p>
        {request.redirect.loopback && !app.local && (
          <Warning title="A program on this device receives the access">
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

      {request.previous && (
        <Fact label={live ? "The access it holds now" : "The access it held"}>
          <ul className="flex flex-col gap-0.5">
            {request.previous.scopes.map((s) => (
              <li
                key={s}
                className="font-mono text-xs [overflow-wrap:anywhere]"
              >
                {s}
              </li>
            ))}
          </ul>
        </Fact>
      )}
    </>
  )
}

/** The app's `authorization_details` as it sent them, to read and copy. */
function RawDetails({ request }: { request: AuthorizationRequest }) {
  const json = JSON.stringify(request.details, null, 2)
  return (
    <Fact label="Authorization details, as the app sent them">
      <div className="relative">
        <pre className="max-h-72 overflow-auto rounded-lg border border-border bg-muted/40 p-3 pr-9 font-mono text-xs">
          {json}
        </pre>
        <CopyButton
          value={json}
          label="Copy the authorization details"
          className="absolute top-2 right-2"
        />
      </div>
    </Fact>
  )
}

/** When the app's newest access was approved, and until when. */
function previousWords(previous: AuthorizationPrevious, live: boolean): string {
  const approved = `You approved it on ${dayWords(new Date(previous.approvedAt))}`
  if (!previous.expiresAt)
    return `${approved}; that access lasts until you revoke it.`
  const ends = dayWords(new Date(previous.expiresAt))
  return live
    ? `${approved}; that access ends on ${ends}.`
    : `${approved}; that access ended on ${ends}.`
}

function Approval({
  id,
  request,
  reread,
  onSettingUp,
  readError,
}: {
  id: string
  request: AuthorizationRequest
  /** Read the request again; the answer is the newest the page holds. */
  reread: () => Promise<AuthorizationRequest | undefined>
  /** The approval was taken and its set up runs in the background. */
  onSettingUp: (rows?: AuthorizationSetup[]) => void
  /** The latest read of the request failed, while the page still shows an
   * earlier one. */
  readError: unknown
}) {
  const { app } = request
  const [technical, setTechnical] = useTechnicalDetails()
  // Every scope starts checked; the set holds the ones the owner unchecked.
  const [unchecked, setUnchecked] = useState<ReadonlySet<string>>(
    () => new Set()
  )
  const [lifetime, setLifetime] = useState(request.defaultLifetimeDays)
  // What moved when the server refused the plan the page had shown.
  const [moved, setMoved] = useState<string[] | null>(null)
  const chosen = request.scopes
    .filter((s) => !unchecked.has(s.scope))
    .map((s) => s.scope)

  const fold = foldRequest(request)
  const full = request.scopes.some((s) => s.verb === "full")
  const { previous } = request
  const webhooks = webhookTriggers(request.setup)
  const blocked = fold.blocks.length > 0

  const settingUp = request.state === "setting-up"
  const approved = request.state === "approved"
  const next = approved ? request.next : undefined
  const nextOk = isWebAddress(next)
  useEffect(() => {
    if (nextOk && next) window.location.assign(next)
  }, [nextOk, next])

  const answer = useMutation({
    mutationFn: async (input: {
      verdict: Verdict
      approval?: AuthorizationApproval
    }): Promise<Outcome> => {
      if (input.verdict === "deny" || !input.approval) {
        const reply = await denyAuthorization(id)
        return { kind: "go", to: followable(reply.redirect) }
      }
      const before = request.setup
      try {
        const reply = await approveAuthorization(id, input.approval)
        if (!reply.redirect && reply.state === "setting-up")
          return { kind: "setting-up", rows: reply.setup }
        return { kind: "go", to: followable(reply.redirect) }
      } catch (error) {
        // A 409 is a request that moved: its plan changed, or another answer
        // reached it first. The read after it says which.
        if (!(error instanceof ApiError && error.status === 409)) throw error
        const fresh = await reread()
        if (
          !fresh ||
          (fresh.state === "pending" &&
            fresh.planHash === input.approval.planHash)
        )
          throw error
        return { kind: "moved", before, fresh }
      }
    },
    onMutate: () => setMoved(null),
    onSuccess: (outcome) => {
      if (outcome.kind === "go") window.location.assign(outcome.to)
      else if (outcome.kind === "setting-up") onSettingUp(outcome.rows)
      else if (outcome.fresh.state === "pending")
        setMoved(planChanges(outcome.before, outcome.fresh.setup))
    },
  })

  const gone = isGone(answer.error) || isGone(readError)
  const leaving =
    (answer.isSuccess && answer.data.kind === "go") || (approved && nextOk)
  // After an answer the page is on its way out, and while a set up runs the
  // request is not the owner's to answer again: nothing may be sent twice.
  const settled = answer.isPending || leaving || gone || settingUp || approved
  const pendingVerdict = answer.isPending ? answer.variables.verdict : undefined
  const failure = request.state === "pending" ? request.failure : undefined
  const steps = settingUp ? progressWords(request.setup) : undefined
  const approveLabel = failure
    ? "Try again"
    : fold.toDo > 0
      ? "Approve and set up"
      : "Approve"

  function toggle(scope: string, checked: boolean) {
    setUnchecked((prev) => {
      const next = new Set(prev)
      if (checked) next.delete(scope)
      else next.add(scope)
      return next
    })
  }

  function approve() {
    answer.mutate({
      verdict: "approve",
      approval: {
        scopes: chosen,
        lifetimeDays: lifetime,
        ...(request.planHash ? { planHash: request.planHash } : {}),
      },
    })
  }

  const scopeRow = (s: (typeof request.scopes)[number]) => (
    <ScopeRow
      key={s.scope}
      scope={s}
      repository={request.repository}
      checked={!unchecked.has(s.scope)}
      disabled={settled}
      technical={technical}
      onChange={(checked) => toggle(s.scope, checked)}
    />
  )
  const setupRow = ({ row, index, grantHeld }: SetupLine) => (
    <SetupRow
      key={index}
      row={row}
      webhooks={webhooks}
      grantHeld={grantHeld}
      failure={failure?.row === index ? failure.message : undefined}
      technical={technical}
    />
  )
  const appTitle = app.local ? "An app on this device" : app.host

  return (
    <Card>
      <CardHeader className="gap-1.5">
        <p className="text-xs text-muted-foreground">
          {fold.reauth
            ? "An app you approved asks "
            : "An app wants to connect to "}
          <span className="[overflow-wrap:anywhere] text-foreground">
            {request.repository}
          </span>
          {fold.reauth && " again"}
        </p>
        <h1 className="font-heading text-xl leading-snug font-semibold [overflow-wrap:anywhere]">
          {fold.reauth
            ? reauthorizationHeading(fold.newAccess, fold.toDo)
            : appTitle}
        </h1>
        {fold.reauth && (
          <p className="font-medium [overflow-wrap:anywhere]">{appTitle}</p>
        )}
        {app.name && (
          <p className="text-xs text-muted-foreground">
            It calls itself{" "}
            <span className="[overflow-wrap:anywhere] text-foreground">
              {app.name}
            </span>
          </p>
        )}
        {technical && (
          <p className="text-xs text-muted-foreground">
            <span>Client id </span>
            <span className="font-mono [overflow-wrap:anywhere]">
              {app.clientId}
            </span>
          </p>
        )}
        {previous && (
          <p className="text-xs text-muted-foreground">
            {previousWords(previous, fold.live)}
          </p>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {full && (
          <Warning title="It asks for full access">
            Approving is the same as signing it in with your password: it can do
            everything you can, including revoking your other sessions. Approve
            only an app you trust that much.
          </Warning>
        )}
        {technical ? (
          <AppFacts request={request} live={fold.live} />
        ) : (
          <AccessWarning request={request} />
        )}

        {fold.live && fold.newAccess + fold.toDo === 0 && (
          <p>
            It asks for nothing new: the access you approved before, for another
            stretch.
          </p>
        )}

        {fold.fresh.length > 0 && (
          <Fact label="It will be able to">
            {!full && (
              <p className="text-xs text-muted-foreground">
                Uncheck anything you don’t want to allow.
              </p>
            )}
            <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
              {fold.fresh.map(scopeRow)}
            </ul>
          </Fact>
        )}

        {fold.setup.length > 0 && (
          <Fact label="It will set up on your server">
            <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
              {fold.setup.map(setupRow)}
            </ul>
          </Fact>
        )}

        {fold.allowed.length + fold.folded.length > 0 && (
          <section aria-label="Already allowed">
            <Collapsible className="flex flex-col gap-2">
              <CollapsibleTrigger className="group flex w-fit cursor-pointer items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground">
                <ChevronRightIcon
                  aria-hidden
                  className="size-3 transition-transform group-data-[panel-open]:rotate-90"
                />
                Already allowed ({fold.allowed.length + fold.folded.length})
              </CollapsibleTrigger>
              <CollapsibleContent>
                <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
                  {fold.allowed.map(scopeRow)}
                  {fold.folded.map(setupRow)}
                </ul>
              </CollapsibleContent>
            </Collapsible>
          </section>
        )}

        {full ? (
          <Fact label="Its changes appear as">
            <p>Yours, as when you sign in, not under the app’s own name.</p>
          </Fact>
        ) : (
          fold.writes &&
          (technical ? (
            <Fact label="Its changes appear as">
              <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
                {app.actor}
              </p>
              <p className="text-xs text-muted-foreground">
                Your history shows what this app changed under this name, never
                as yours.
              </p>
              <AutomationWarning />
            </Fact>
          ) : (
            <AutomationWarning />
          ))
        )}

        {/* A blocked row holds the whole approval back, so nothing runs
            until it clears. */}
        {fold.toDo > 0 && !blocked && request.state === "pending" && (
          <p className="text-xs text-muted-foreground">{CONTINUES}</p>
        )}

        {technical && request.details.length > 0 && (
          <RawDetails request={request} />
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
              : `It ends on ${lifetimeEnd(lifetime)}. After that, the app has to ask you again. You can revoke it sooner in Settings, under Apps.`}
          </p>
        </Fact>

        <Separator />

        {/* A failure's message sits under the row it stopped at; one that
            stopped at no row has nowhere else to say it. */}
        {failure && (
          <Problem title={failureWords(fold.failedRow)}>
            {!fold.failedRow && asSentence(failure.message)}
          </Problem>
        )}
        {moved && (
          <Warning title="What it sets up changed since this page was read">
            {moved.length > 0 ? (
              <ul className="list-disc pl-4">
                {moved.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
            ) : (
              <p>The details behind it changed.</p>
            )}
            <p>Check it again, then approve.</p>
          </Warning>
        )}
        {gone ? (
          <Refusal
            error={isGone(answer.error) ? answer.error : readError}
            verdict="read"
          />
        ) : (
          answer.isError && (
            <Refusal error={answer.error} verdict={answer.variables.verdict} />
          )
        )}
        {approved && !nextOk && !gone && (
          <Problem title="This request was approved, but the console can’t take you back to the app.">
            {NOT_A_WEB_PAGE}
          </Problem>
        )}
        {/* One live region, there from the first render: a region inserted
            already filled is not announced, and the rows' progress is
            outside it. The visible lines repeat it for the eye alone. */}
        <p role="status" aria-live="polite" className="sr-only">
          {leaving
            ? "Taking you back to the app…"
            : settingUp && !gone
              ? [SETTING_UP, steps].filter(Boolean).join(" ")
              : ""}
        </p>
        {settingUp && !gone && (
          <p
            aria-hidden
            className="flex items-center gap-2 text-sm text-muted-foreground"
          >
            <Spinner aria-hidden role={undefined} />
            {SETTING_UP}
          </p>
        )}
        {settingUp && readError !== undefined && !gone && (
          <p className="text-xs text-muted-foreground">
            The latest check on it didn’t answer; the page keeps asking.
          </p>
        )}
        {leaving && (
          <p aria-hidden className="text-sm text-muted-foreground">
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
              disabled={settled || chosen.length === 0 || blocked}
              onClick={approve}
            >
              {pendingVerdict === "approve" && <Spinner />}
              {approveLabel}
            </Button>
          </div>
          {chosen.length === 0 && !settled && (
            <p className="text-right text-xs text-muted-foreground">
              Check at least one, or deny the request.
            </p>
          )}
          {blocked && !settled && (
            <div className="flex flex-wrap items-center justify-end gap-2 text-right text-xs text-muted-foreground">
              <span>{blockedWords(fold.blocks)}</span>
              <Button variant="outline" size="xs" onClick={() => void reread()}>
                Check again
              </Button>
            </div>
          )}
          {fold.reauth && !settled && (
            <p className="text-right text-xs text-muted-foreground">
              {fold.live
                ? "If you deny, the access you approved before keeps working."
                : "Its earlier access has ended; denying leaves it without access."}
            </p>
          )}
          {!settled && (
            <p
              className="text-right text-xs text-faint"
              title={shortDateTime(request.expiresAt)}
            >
              This request lapses {relativeTime(request.expiresAt)} if you do
              not answer it.
            </p>
          )}
        </div>
      </CardContent>
      <CardFooter className="justify-between gap-3">
        <span className="text-xs text-muted-foreground">
          Show technical details
        </span>
        <ToggleSwitch
          checked={technical}
          onChange={setTechnical}
          label="Show technical details"
        />
      </CardFooter>
    </Card>
  )
}

function AutomationWarning() {
  return (
    <Warning title="Your automations run on its changes">
      Your triggers and mappings respond to its changes like any other. They may
      run code you installed, call services that charge you, or change records
      this app cannot reach itself.
    </Warning>
  )
}

function AuthorizeDoor() {
  const { request: id } = authorizeRoute.useParams()
  const client = useQueryClient()
  const [technical] = useTechnicalDetails()
  const queryKey = ["authorization", id]
  // Read once, right after an approval whose set up runs, and every second
  // while it runs: a refetch after any other answer would only find the
  // request gone. A dropped connection or a busy server is still retried,
  // the console's default for a read; a 404 never is.
  const authorization = useQuery({
    queryKey,
    queryFn: ({ signal }) => getAuthorization(id, signal),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: (query) =>
      isSettingUp(query.state.data) && !isGone(query.state.error)
        ? SETUP_POLL_MS
        : false,
  })

  return (
    <Door wide={technical}>
      {authorization.data ? (
        <Approval
          id={id}
          request={authorization.data}
          reread={async () => (await authorization.refetch()).data}
          onSettingUp={(rows) => {
            // The answer carries the rows the server runs, each waiting; the
            // read right after it brings their progress. It joins a read in
            // flight rather than cancel it, since the read that hands over
            // the code is the request's last.
            client.setQueryData<AuthorizationRequest>(
              queryKey,
              (old) =>
                old && {
                  ...old,
                  state: "setting-up",
                  failure: undefined,
                  setup: rows ?? old.setup,
                }
            )
            void authorization.refetch({ cancelRefetch: false })
          }}
          readError={authorization.isError ? authorization.error : undefined}
        />
      ) : authorization.isPending ? (
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
      ) : (
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
      )}
    </Door>
  )
}

/** The page reads and writes the console's one Technical details switch, so
 * it mounts the preferences the shell would; it renders outside the shell,
 * so without its sidebar. */
export function AuthorizePage() {
  useDocumentTitle("Authorize an app")
  return (
    <ConsolePreferencesProvider sidebar={false}>
      <AuthorizeDoor />
    </ConsolePreferencesProvider>
  )
}
