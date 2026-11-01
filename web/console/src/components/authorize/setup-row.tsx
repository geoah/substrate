/** One setup row on the approval page (docs/plans/app-setup.md): what
 * approving installs, updates, creates or turns on, by its catalog name and
 * description, with the code it runs, what it brings first, the account's
 * every value and, while a set up runs, how far it got. The technical view
 * adds every identifier and version behind it. */

import type { ReactNode } from "react"
import {
  ArrowUpIcon,
  BanIcon,
  CheckIcon,
  CircleCheckIcon,
  CircleDashedIcon,
  CircleXIcon,
  DownloadIcon,
  ExternalLinkIcon,
  PlusIcon,
  ZapIcon,
} from "lucide-react"

import { Mono, Term } from "@/components/authorize/parts"
import { Spinner } from "@/components/ui/spinner"
import type {
  AuthorizationAccount,
  AuthorizationPackage,
  AuthorizationSetup,
  AuthorizationWebhookPlan,
  SetupAction,
  SetupProgress,
} from "@/lib/api/types"
import {
  accountName,
  asSentence,
  chainWords,
  codeWords,
  consolePath,
  problemWords,
  propertyLabel,
  propertyValue,
  setupTitle,
  webhookTriggers,
} from "@/lib/app-setup"
import { cn } from "@/lib/utils"

const ACTION_ICONS: Partial<Record<SetupAction, typeof CheckIcon>> = {
  install: DownloadIcon,
  upgrade: ArrowUpIcon,
  create: PlusIcon,
  arm: ZapIcon,
  none: CheckIcon,
  blocked: BanIcon,
}

const PROGRESS_WORDS: Record<SetupProgress, string> = {
  waiting: "Waiting",
  running: "Setting up",
  done: "Done",
  failed: "Stopped",
}

const STEP_WORDS: Record<SetupAction, string> = {
  install: "installs",
  upgrade: "updates",
  create: "creates",
  arm: "turns on",
  none: "in place",
  blocked: "blocked",
  unknown: "unknown",
}

function RowIcon({
  action,
  progress,
}: {
  action: SetupAction
  progress: SetupProgress | undefined
}) {
  const className = "mt-0.5 size-4 shrink-0"
  if (progress === "running")
    return <Spinner aria-hidden role={undefined} className={className} />
  if (progress === "done")
    return <CircleCheckIcon aria-hidden className={cn(className, "text-ok")} />
  if (progress === "failed")
    return (
      <CircleXIcon aria-hidden className={cn(className, "text-destructive")} />
    )
  if (progress === "waiting")
    return (
      <CircleDashedIcon
        aria-hidden
        className={cn(className, "text-muted-foreground")}
      />
    )
  const Icon = ACTION_ICONS[action] ?? CheckIcon
  return (
    <Icon
      aria-hidden
      className={cn(
        className,
        action === "blocked" ? "text-destructive" : "text-muted-foreground"
      )}
    />
  )
}

/** The row's state beside its title: how far a set up got, or that the row
 * is in place or cannot run. */
function statusWords(
  action: SetupAction,
  progress: SetupProgress | undefined
): string | undefined {
  if (progress) return PROGRESS_WORDS[progress]
  if (action === "none") return "In place"
  if (action === "blocked") return "Can’t be done yet"
  return undefined
}

function Facts({ children }: { children: ReactNode }) {
  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-xs">
      {children}
    </dl>
  )
}

function version(n: number): string {
  return n > 0 ? `version ${n}` : "none"
}

function PackageFacts({ pkg }: { pkg: AuthorizationPackage }) {
  return (
    <Facts>
      <Term name="Package">
        <Mono>{pkg.id}</Mono>
      </Term>
      {pkg.tier && <Term name="Tier">{pkg.tier}</Term>}
      <Term name="The app needs">version {pkg.required} or later</Term>
      <Term name="You hold">{version(pkg.held)}</Term>
      <Term name="This server ships">{version(pkg.shipped)}</Term>
      {pkg.requires && pkg.requires.length > 0 && (
        <Term name="Brings first">
          <ul className="flex flex-col gap-0.5">
            {pkg.requires.map((s) => (
              <li key={s.id}>
                <Mono>{s.id}</Mono>: {STEP_WORDS[s.action]}, you hold{" "}
                {version(s.held)}, this server ships {version(s.shipped)}
              </li>
            ))}
          </ul>
        </Term>
      )}
      {pkg.kinds && pkg.kinds.length > 0 && (
        <Term name="Kinds">
          <ul className="flex flex-col gap-0.5">
            {pkg.kinds.map((k) => (
              <li key={k.identity}>
                <Mono>{k.identity}</Mono>
                {k.label && <span>: {k.label.plural}</span>}
                {k.purpose && (
                  <span className="text-muted-foreground"> ({k.purpose})</span>
                )}
              </li>
            ))}
          </ul>
        </Term>
      )}
      {pkg.functions && pkg.functions.length > 0 && (
        <Term name="Functions">
          <ul className="flex flex-col gap-0.5">
            {pkg.functions.map((f) => (
              <li key={f.id}>
                <Mono>{f.id}</Mono>
                {f.writes && f.writes.length > 0 ? (
                  <>
                    , may write{" "}
                    {f.writes.map((w, i) => (
                      <span key={w}>
                        {i > 0 && ", "}
                        <Mono>{w}</Mono>
                      </span>
                    ))}
                  </>
                ) : (
                  ", writes nothing"
                )}
              </li>
            ))}
          </ul>
        </Term>
      )}
      {pkg.triggers && pkg.triggers.length > 0 && (
        <Term name="Triggers">
          <ul className="flex flex-col gap-0.5">
            {pkg.triggers.map((t) => (
              <li key={t.id}>
                <Mono>{t.id}</Mono>
                {t.webhook ? ", a webhook" : ""},{" "}
                {t.enabled ? "ships on" : "ships off"}
                {t.callable && (
                  <>
                    , runs <Mono>{t.callable}</Mono>
                  </>
                )}
              </li>
            ))}
          </ul>
        </Term>
      )}
    </Facts>
  )
}

/** The account by kind and id; the values it is created with where
 * approving creates it, since one in place is never written. */
function AccountFacts({
  account,
  inPlace,
}: {
  account: AuthorizationAccount
  inPlace: boolean
}) {
  return (
    <Facts>
      <Term name="Kind">
        <Mono>{account.kind}</Mono>
      </Term>
      <Term name="Id">
        <Mono>{account.id}</Mono>
      </Term>
      {!inPlace &&
        Object.entries(account.properties ?? {}).map(([key, value]) => (
          <Term key={key} name={key}>
            <Mono>{JSON.stringify(value)}</Mono>
          </Term>
        ))}
    </Facts>
  )
}

function WebhookFacts({ webhook }: { webhook: AuthorizationWebhookPlan }) {
  return (
    <Facts>
      <Term name="Trigger">
        <Mono>{webhook.trigger}</Mono>
      </Term>
      {webhook.package && (
        <Term name="Package">
          <Mono>{webhook.package}</Mono>
        </Term>
      )}
      <Term name="Turned on">{webhook.enabled ? "Yes" : "No"}</Term>
      <Term name="Key">
        {webhook.keyed
          ? "It has one, which stays as it is."
          : "None yet: approving sets one, so the webhook is never open."}{" "}
        The key is never shared: the app delivers with its own access.
      </Term>
    </Facts>
  )
}

/** Every value the account is created with, by its property's name. */
function AccountValues({ account }: { account: AuthorizationAccount }) {
  const entries = Object.entries(account.properties ?? {})
  if (entries.length === 0) return null
  return (
    <dl
      aria-label={`The values of ${accountName(account)}`}
      className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-0.5 text-xs"
    >
      {entries.map(([key, value]) => (
        <Term key={key} name={propertyLabel(key)}>
          {propertyValue(value)}
        </Term>
      ))}
    </dl>
  )
}

export function SetupRow({
  row,
  rows,
  progress,
  failure,
  technical,
}: {
  row: AuthorizationSetup
  rows: readonly AuthorizationSetup[]
  progress: SetupProgress | undefined
  /** Why the set up stopped at this row, when it did. */
  failure: string | undefined
  technical: boolean
}) {
  const title = setupTitle(row, rows)
  const status = statusWords(row.action, progress)
  const fix = consolePath(row.fix)
  const code = row.package && codeWords(row.package, webhookTriggers(rows))
  const chain = row.package && chainWords(row.package)
  const inPlace = row.action === "none"
  // The everyday view words a block itself where the versions say what it
  // is; the technical view keeps the server's own sentence.
  const problem = technical
    ? row.problem && asSentence(row.problem)
    : problemWords(row)
  return (
    <li aria-label={title} className="flex gap-3 px-3 py-3">
      <RowIcon action={row.action} progress={progress} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
          <p className="font-medium [overflow-wrap:anywhere]">{title}</p>
          {status && (
            <span className="text-xs text-muted-foreground">{status}</span>
          )}
        </div>
        {row.package?.description && (
          <p className="text-xs text-muted-foreground">
            {row.package.description}
          </p>
        )}
        {code && <p className="text-xs">{code}</p>}
        {chain && <p className="text-xs">{chain}</p>}
        {row.account &&
          (inPlace ? (
            <p className="text-xs text-muted-foreground">
              Your existing account stays as it is.
            </p>
          ) : (
            <AccountValues account={row.account} />
          ))}
        {row.action === "blocked" && (
          <div className="flex flex-col gap-0.5 text-xs">
            {problem && <p className="text-destructive">{problem}</p>}
            {fix && (
              <a
                href={fix}
                target="_blank"
                rel="noreferrer"
                className="inline-flex w-fit items-center gap-1 text-primary underline-offset-4 hover:underline"
              >
                Fix this in the console
                <ExternalLinkIcon aria-hidden className="size-3" />
              </a>
            )}
          </div>
        )}
        {failure && (
          <p className="text-xs text-destructive">{asSentence(failure)}</p>
        )}
        {technical && (
          <div className="pt-1">
            {row.package && <PackageFacts pkg={row.package} />}
            {row.account && (
              <AccountFacts account={row.account} inPlace={inPlace} />
            )}
            {row.webhook && <WebhookFacts webhook={row.webhook} />}
          </div>
        )}
      </div>
    </li>
  )
}
