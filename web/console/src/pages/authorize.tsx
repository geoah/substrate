/** An app's approval page (`/authorize/$request`, decision 0194). An app sent
 * the owner here through `GET /oauth/authorize`; the server parked its request
 * under the id in the path. The page leads with what the app will be able to
 * do and what approving sets up on the server, in plain words
 * (docs/plans/app-setup.md), and the console's one Technical details switch
 * (decision 0131) adds every identifier behind them: beside each scope and
 * setup row, and the rest in one block under the answer. The owner approves
 * some of the scopes for one lifetime, or denies the lot.
 *
 * A denial, and an approval with nothing to set up, carry the URL to go to
 * (the app's redirect URI with the code or with the refusal). An approval
 * that sets something up answers `202`, and the page reads the request every
 * second until it is approved and carries `next`, or stopped with a failure.
 * The page navigates with `window.location.assign`: the console's CSP has
 * `form-action 'self'`, so a form post that redirects off the origin would
 * be blocked. An error never navigates anywhere.
 *
 * An AI assistant's request (`mcp`, decision 0230) is approved whole, like
 * `full`: the page says where its access works, what it can and cannot do,
 * that code it writes reaches the owner's secrets, and whose name its
 * changes carry, and offers "Until you revoke it" first, since an assistant
 * holds no refresh token. */

import { useEffect, useState, type ReactNode } from "react"
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import { BoxesIcon, ChevronRightIcon } from "lucide-react"

import { Caution, Fact, Problem, Warning } from "@/components/authorize/parts"
import {
  PublisherFacts,
  PublisherLines,
} from "@/components/authorize/publisher"
import { HintMismatch } from "@/components/authorize/hint-mismatch"
import { ScopeRow } from "@/components/authorize/scope-row"
import { ProviderPicker } from "@/components/authorize/provider-picker"
import { PackageChoices } from "@/components/authorize/package-choices"
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
import { useSecretMutation } from "@/hooks/use-secret-mutation"
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
  type AuthorizationSettingValue,
} from "@/lib/api/types"
import {
  approvalHeading,
  asSentence,
  blockedWords,
  dayWords,
  failureWords,
  foldRequest,
  lifetimeEnd,
  planChanges,
  progressWords,
  providerNeedWords,
  webhookTriggers,
  type SetupLine,
} from "@/lib/app-setup"
import { appSubject, appTitle as titleOf } from "@/lib/app-publisher"
import {
  ASSISTANT_CAN,
  ASSISTANT_CANNOT,
  ASSISTANT_CODE_SENTENCE,
} from "@/lib/app-scopes"
import { assistantAddress } from "@/lib/assistants"
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

/** What an assistant's approval lets in (decision 0230): where its access
 * works, the lines Settings' AI assistants page shows too, and its code's
 * reach, which the owner chose to say rather than guard. */
function AssistantReach({ technical }: { technical: boolean }) {
  return (
    <>
      <Fact label="Where it works">
        <p>Only at this server’s address for AI assistants:</p>
        <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
          {assistantAddress()}
        </p>
        <p className="text-xs text-muted-foreground">
          Its access works nowhere else: not in this console, and not through
          the API.
        </p>
      </Fact>
      <Fact label="It will be able to">
        <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
          {ASSISTANT_CAN.map((line) => (
            <li key={line} className="px-3 py-2.5">
              {line}.
            </li>
          ))}
        </ul>
        {technical && (
          <p className="font-mono text-xs text-muted-foreground">mcp</p>
        )}
      </Fact>
      <Fact label="It won’t be able to">
        <ul className="flex list-disc flex-col gap-0.5 pl-4">
          {ASSISTANT_CANNOT.map((line) => (
            <li key={line}>{line}.</li>
          ))}
        </ul>
      </Fact>
      <Warning title="Its code can reach your secrets">
        {ASSISTANT_CODE_SENTENCE}.
      </Warning>
    </>
  )
}

/** The name an assistant's changes carry in History: the token's claimed
 * name, which a verified publisher's name for the app replaces. */
function assistantNameWords(request: AuthorizationRequest): string {
  const { app } = request
  if (app.verified) return app.publisher.appName || app.name || app.host
  return app.name ? `“${app.name}”, the name it gives itself` : app.host
}

/** What the Technical details switch adds, in one block under the answer
 * rather than between the facts the owner decides on: the client id, the
 * publisher check, an unverified app's claims, where the code goes, the
 * actor its changes carry, the access it holds or held, and the details as
 * sent. The warnings stay above, in both views. A verified app's own claims
 * are left out: its publisher's document speaks for it. */
function TechnicalFacts({
  request,
  live,
  actor,
}: {
  request: AuthorizationRequest
  live: boolean
  /** Its changes carry the app's own actor, which nothing above names. */
  actor: boolean
}) {
  const { app } = request
  return (
    <section aria-label="Technical details" className="flex flex-col gap-5">
      <Fact label="Client id">
        <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
          {app.clientId}
        </p>
      </Fact>

      <PublisherFacts request={request} />

      {!app.verified && (app.name || app.uri) && (
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
      </Fact>

      {actor && (
        <Fact label="Its changes appear as">
          <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
            {app.appearsAs}
          </p>
          <p className="text-xs text-muted-foreground">
            Your history shows what this app changed under this name, never as
            yours.
          </p>
        </Fact>
      )}

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

      {request.details.length > 0 && <RawDetails request={request} />}
    </section>
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
  onChecked,
  planning,
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
  /** The scopes left checked changed: the set up is planned against them
   * (decision 0215), so the page reads the request again with them. */
  onChecked: (scopes: string[]) => void
  /** That read is still on its way: the plan shown is the last one, so
   * nothing is approved until the new one is in. */
  planning: boolean
}) {
  const { app } = request
  const [technical, setTechnical] = useTechnicalDetails()
  // Every scope starts checked; the set holds the ones left unchecked.
  const [unchecked, setUnchecked] = useState<ReadonlySet<string>>(
    () => new Set()
  )
  const [providerChoices, setProviderChoices] = useState<
    Record<string, string>
  >({})
  // An optional add-on starts unchecked: the owner checks the ones to
  // approve (decision 0216), and an unchecked one is left out of the plan.
  const [addons, setAddons] = useState<string[]>([])
  const [settings, setSettings] = useState<AuthorizationSettingValue[]>([])
  const activeRows = request.setup.filter(
    (r) => !r.package?.addonFor || addons.includes(r.package.id)
  )
  const selectedSettings = settings.filter((v) =>
    activeRows.some(
      (r) =>
        r.action !== "wait" &&
        r.package?.settings?.some((s) => s.id === v.id && s.secret === v.secret)
    )
  )
  const missingSetting = activeRows.some(
    (r) =>
      r.action !== "wait" &&
      r.package?.settings?.some(
        (s) =>
          s.required &&
          !selectedSettings.some(
            (v) => v.id === s.id && v.secret === s.secret && v.value !== ""
          )
      )
  )
  const mcp = request.scopes.some((s) => s.verb === "mcp")
  // An assistant holds no refresh token, so one that lapses has to be
  // approved again: until revoked is offered first.
  const [lifetime, setLifetime] = useState(() =>
    mcp && request.lifetimeDays.includes(0) ? 0 : request.defaultLifetimeDays
  )
  // What moved when the server refused the plan the page had shown.
  const [moved, setMoved] = useState<string[] | null>(null)
  const chosen = request.scopes
    .filter((s) => !unchecked.has(s.scope))
    .map((s) => s.scope)

  const fold = foldRequest(request)
  const nothingNew = fold.live && fold.newAccess + fold.toDo === 0
  const full = request.scopes.some((s) => s.verb === "full")
  const { previous } = request
  const webhooks = webhookTriggers(request.setup)
  // An unchecked add-on is left out of the plan, so its inputs need no
  // choice and its block holds nothing back.
  const providerInputs = activeRows.flatMap(
    (r) => r.package?.providerInputs ?? []
  )
  const activeBlocks = activeRows.flatMap((r) => (r.blocked ? [r.blocked] : []))
  const missingInputs = providerInputs.filter(
    (input) =>
      !(providerChoices[input.package + "/" + input.input] ?? input.selected)
  )
  const missingProvider = missingInputs.length > 0
  const blocked = activeBlocks.length > 0 || missingSetting

  const settingUp = request.state === "setting-up"
  const approved = request.state === "approved"
  const next = approved ? request.next : undefined
  const nextOk = isWebAddress(next)
  useEffect(() => {
    if (nextOk && next) window.location.assign(next)
  }, [nextOk, next])

  // The answer the owner gave last, for the spinner and the refusal: the
  // mutation holds no variables.
  const [verdict, setVerdict] = useState<Verdict>("approve")
  // An approval carries the values of the settings and secrets the package
  // needs, so it goes through useSecretMutation and nothing of them stays
  // in the mutation cache.
  const answer = useSecretMutation({
    mutationKey: ["authorize", id],
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
  const pendingVerdict = answer.isPending ? verdict : undefined
  const failure = request.state === "pending" ? request.failure : undefined
  const steps = settingUp ? progressWords(request.setup) : undefined
  const approveLabel = failure
    ? "Try again"
    : fold.toDo > 0
      ? "Approve and set up"
      : nothingNew
        ? "Keep access"
        : "Approve"

  function toggle(scope: string, checked: boolean) {
    const next = new Set(unchecked)
    if (checked) next.delete(scope)
    else next.add(scope)
    setUnchecked(next)
    // Only a request with set up has a plan to make again.
    if (request.details.length > 0)
      onChecked(
        request.scopes.filter((s) => !next.has(s.scope)).map((s) => s.scope)
      )
  }

  function send(input: Parameters<typeof answer.mutate>[0]) {
    setVerdict(input.verdict)
    answer.mutate(input)
  }

  function approve() {
    send({
      verdict: "approve",
      approval: {
        scopes: chosen,
        ...(addons.length ? { addons } : {}),
        ...(selectedSettings.length ? { settings: selectedSettings } : {}),
        lifetimeDays: lifetime,
        ...(request.planHash ? { planHash: request.planHash } : {}),
        ...(providerInputs.length
          ? {
              providerChoices: providerInputs.map((input) => ({
                package: input.package,
                input: input.input,
                provider:
                  providerChoices[input.package + "/" + input.input] ??
                  input.selected ??
                  "",
              })),
            }
          : {}),
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
  // A package's choices sit inside its row: whether an add-on is included,
  // then the AI provider for each of its inputs and its settings, which a
  // row left out does not ask for.
  const setupRow = ({ row, index, grantHeld }: SetupLine) => {
    const pkg = row.package
    const included = !pkg?.addonFor || addons.includes(pkg.id)
    return (
      <SetupRow
        key={index}
        row={row}
        webhooks={webhooks}
        grantHeld={grantHeld}
        failure={failure?.row === index ? failure.message : undefined}
        technical={technical}
      >
        {pkg && (
          <PackageChoices
            pkg={pkg}
            waiting={row.action === "wait"}
            selected={included}
            disabled={settled}
            onSelect={(checked) =>
              setAddons((old) =>
                checked ? [...old, pkg.id] : old.filter((id) => id !== pkg.id)
              )
            }
            values={settings}
            onValues={setSettings}
          />
        )}
        {included &&
          (pkg?.providerInputs ?? []).map((input) => (
            <ProviderPicker
              key={input.package + "/" + input.input}
              input={input}
              inputs={providerInputs}
              value={
                providerChoices[input.package + "/" + input.input] ??
                input.selected ??
                ""
              }
              disabled={settled}
              technical={technical}
              onChange={(value) =>
                setProviderChoices((previous) => ({
                  ...previous,
                  [input.package + "/" + input.input]: value,
                }))
              }
            />
          ))}
      </SetupRow>
    )
  }
  return (
    <Card>
      <CardHeader className="gap-1.5">
        <div className="flex flex-col gap-0.5">
          <p className="font-medium [overflow-wrap:anywhere]">
            {titleOf(request)}
          </p>
          <PublisherLines request={request} />
        </div>
        <h1 className="mt-2 font-heading text-xl leading-snug font-semibold [overflow-wrap:anywhere]">
          {approvalHeading(appSubject(request, mcp), request.repository, fold)}
        </h1>
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
            {!app.verified &&
              " This app is unverified: nothing confirms which program receives a token equal to your password."}
          </Warning>
        )}
        {mcp && (
          <Warning title="It asks to work with nearly everything you have">
            It can see and change nearly everything you can, and run your code.
            Approve only an assistant you trust that much.
          </Warning>
        )}
        {/* A hosted assistant's client id and callback are every one of its
            accounts', so a link someone else started lands here the same
            way and hands them the access. */}
        {mcp && !app.local && !request.redirect.loopback && (
          <Warning title="Approve only a sign-in you just started">
            Every {app.host} account connects with this same app and returns to
            the same address, so nothing here tells your sign-in from someone
            else’s. If you didn’t just connect this assistant yourself, deny it.
          </Warning>
        )}
        <AccessWarning request={request} />

        {nothingNew && (
          <p>
            It asks for nothing new: the access you approved before, for another
            stretch.
          </p>
        )}

        {mcp && <AssistantReach technical={technical} />}

        {!mcp && fold.fresh.length > 0 && (
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

        {/* A renewal that asks for nothing new is a decision about exactly
            this list, so it starts open. */}
        {!mcp && fold.allowed.length + fold.folded.length > 0 && (
          <section aria-label="Already allowed">
            <Collapsible
              defaultOpen={nothingNew}
              className="flex flex-col gap-2"
            >
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
        ) : mcp ? (
          <Fact label="Its changes appear as">
            <p className="[overflow-wrap:anywhere]">
              {assistantNameWords(request)}, not you.
            </p>
            {technical && (
              <p className="font-mono text-[13px] [overflow-wrap:anywhere]">
                {app.appearsAs}
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              History shows what it changed under that name, never as yours.
            </p>
          </Fact>
        ) : (
          fold.writes && <AutomationCaution />
        )}

        {/* A blocked row holds the whole approval back, so nothing runs
            until it clears. */}
        {fold.toDo > 0 && !blocked && request.state === "pending" && (
          <p className="text-xs text-muted-foreground">{CONTINUES}</p>
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
              ? `It lasts until you revoke it in Settings, under ${mcp ? "AI assistants" : "Apps"}.`
              : mcp
                ? `It ends on ${lifetimeEnd(lifetime)}. After that, you sign the assistant in again. You can revoke it sooner in Settings, under AI assistants.`
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
          answer.isError && <Refusal error={answer.error} verdict={verdict} />
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
              onClick={() => send({ verdict: "deny" })}
            >
              {pendingVerdict === "deny" && <Spinner />}
              Deny
            </Button>
            <Button
              disabled={
                settled ||
                chosen.length === 0 ||
                blocked ||
                missingProvider ||
                planning
              }
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
              <span>
                {missingSetting
                  ? "Fill every required setting."
                  : blockedWords(activeBlocks)}
              </span>
              <Button variant="outline" size="xs" onClick={() => void reread()}>
                Check again
              </Button>
            </div>
          )}
          {missingProvider && !settled && (
            <p className="text-right text-xs text-muted-foreground">
              {providerNeedWords(missingInputs, providerInputs)}
            </p>
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

        {technical && (
          <>
            <Separator />
            <TechnicalFacts
              request={request}
              live={fold.live}
              actor={!full && !mcp && fold.writes}
            />
          </>
        )}
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

function AutomationCaution() {
  return (
    <Caution title="Your automations run on its changes">
      Your triggers and mappings respond to them like any other, and may run
      code you installed, call services that charge you, or change records this
      app cannot reach itself.
    </Caution>
  )
}

function AuthorizeDoor() {
  const { request: id } = authorizeRoute.useParams()
  const { login_hint: hint } = authorizeRoute.useSearch()
  const client = useQueryClient()
  const [technical] = useTechnicalDetails()
  // The scopes left checked, once the owner changed them: the set up is
  // planned against them (decision 0215), so each change is a read of its
  // own, the page keeping the last answer up while it waits.
  const [checked, setChecked] = useState<string[] | undefined>(undefined)
  const queryKey = ["authorization", id, checked ?? null]
  // Read once, right after an approval whose set up runs, and every second
  // while it runs: a refetch after any other answer would only find the
  // request gone. A dropped connection or a busy server is still retried,
  // the console's default for a read; a 404 never is.
  const authorization = useQuery({
    queryKey,
    queryFn: ({ signal }) => getAuthorization(id, signal, checked),
    placeholderData: keepPreviousData,
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
      {authorization.data &&
        hint &&
        hint !== authorization.data.repository &&
        authorization.data.state === "pending" && (
          <HintMismatch
            hint={hint}
            repository={authorization.data.repository}
          />
        )}
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
          onChecked={setChecked}
          planning={authorization.isPlaceholderData}
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
