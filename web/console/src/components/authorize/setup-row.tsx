/** One setup row on the approval page (docs/plans/app-setup.md): what
 * approving installs, updates, creates or turns on, by its name, versions
 * and description, with the code it and what it brings first run, the
 * account's every value, the access a webhook brings, why it cannot run
 * and, while a set up runs, how far it got. The technical view adds every
 * identifier and version behind it. */

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

import { Disclosure, Mono, Term } from "@/components/authorize/parts"
import { Spinner } from "@/components/ui/spinner"
import type {
  AuthorizationAccount,
  AuthorizationPackage,
  AuthorizationPackageStep,
  AuthorizationSetup,
  AuthorizationWebhookPlan,
  SetupAction,
  SetupProgress,
} from "@/lib/api/types"
import {
  accountName,
  asSentence,
  blockStatus,
  chainLines,
  codeWords,
  consolePath,
  factCounts,
  grantWords,
  packageName,
  propertyLabel,
  propertyValue,
  setupTitle,
  versionWords,
} from "@/lib/app-setup"
import { cn } from "@/lib/utils"

const ACTION_ICONS: Partial<Record<SetupAction, typeof CheckIcon>> = {
  install: DownloadIcon,
  upgrade: ArrowUpIcon,
  create: PlusIcon,
  enable: ZapIcon,
  none: CheckIcon,
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
  enable: "turns on",
  none: "in place",
  unknown: "unknown",
}

function RowIcon({ row }: { row: AuthorizationSetup }) {
  const className = "mt-0.5 size-4 shrink-0"
  if (row.progress === "running")
    return <Spinner aria-hidden role={undefined} className={className} />
  if (row.progress === "done")
    return <CircleCheckIcon aria-hidden className={cn(className, "text-ok")} />
  if (row.progress === "failed")
    return (
      <CircleXIcon aria-hidden className={cn(className, "text-destructive")} />
    )
  if (row.progress === "waiting")
    return (
      <CircleDashedIcon
        aria-hidden
        className={cn(className, "text-muted-foreground")}
      />
    )
  if (row.blocked)
    return <BanIcon aria-hidden className={cn(className, "text-destructive")} />
  const Icon = ACTION_ICONS[row.action] ?? CheckIcon
  return <Icon aria-hidden className={cn(className, "text-muted-foreground")} />
}

/** The row's state beside its title: how far a set up got, that it cannot
 * run and what it waits on, or that it is in place. */
function statusWords(row: AuthorizationSetup): string | undefined {
  if (row.progress) return PROGRESS_WORDS[row.progress]
  if (row.blocked) return blockStatus(row.blocked)
  if (row.action === "none") return "In place"
  return undefined
}

function Facts({ children }: { children: ReactNode }) {
  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-xs">
      {children}
    </dl>
  )
}

function version(n: number | undefined): string {
  return n ? `version ${n}` : "none"
}

/** A package's own kinds, functions and triggers, each counted and opened
 * on demand, so a long closure is not one wall of references. */
function OwnFacts({
  facts,
  of,
}: {
  facts: Pick<AuthorizationPackage, "kinds" | "functions" | "triggers">
  of: string
}) {
  const kinds = facts.kinds ?? []
  const functions = facts.functions ?? []
  const triggers = facts.triggers ?? []
  if (kinds.length + functions.length + triggers.length === 0) return null
  return (
    <div className="flex flex-col gap-1 text-xs">
      {kinds.length > 0 && (
        <Disclosure label="Kinds" count={kinds.length} of={of}>
          <ul className="flex flex-col gap-0.5">
            {kinds.map((k) => (
              <li key={k.identity}>
                <Mono>{k.identity}</Mono>
                {k.label && <span>: {k.label.plural}</span>}
                {k.purpose && (
                  <span className="text-muted-foreground"> ({k.purpose})</span>
                )}
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
      {functions.length > 0 && (
        <Disclosure label="Functions" count={functions.length} of={of}>
          <ul className="flex flex-col gap-1.5">
            {functions.map((f) => (
              <li key={f.id} className="flex flex-col gap-0.5">
                <Mono>{f.id}</Mono>
                {f.writes && f.writes.length > 0 ? (
                  <>
                    <span className="text-muted-foreground">May write</span>
                    <ul className="flex flex-col gap-0.5 pl-3">
                      {f.writes.map((w) => (
                        <li key={w}>
                          <Mono>{w}</Mono>
                        </li>
                      ))}
                    </ul>
                  </>
                ) : (
                  <span className="text-muted-foreground">Writes nothing</span>
                )}
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
      {triggers.length > 0 && (
        <Disclosure label="Triggers" count={triggers.length} of={of}>
          <ul className="flex flex-col gap-0.5">
            {triggers.map((t) => (
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
        </Disclosure>
      )}
    </div>
  )
}

/** A package an install brings first: its name and what it brings, counted,
 * then its id, versions and its own facts. */
function StepFacts({ step }: { step: AuthorizationPackageStep }) {
  const name = packageName(step.id)
  const counts = factCounts(step)
  return (
    <li className="flex flex-col gap-0.5">
      <p>
        <span className="font-medium">{name}</span>
        {counts && <span className="text-muted-foreground"> — {counts}</span>}
      </p>
      <p>
        <Mono>{step.id}</Mono>: {STEP_WORDS[step.action]}, you hold{" "}
        {version(step.held)}, this server ships {version(step.shipped)}
      </p>
      {step.description && (
        <p className="text-muted-foreground">{step.description}</p>
      )}
      {step.blocked && (
        <p className="text-destructive">{asSentence(step.blocked.problem)}</p>
      )}
      <OwnFacts facts={step} of={name} />
    </li>
  )
}

function PackageFacts({ pkg }: { pkg: AuthorizationPackage }) {
  const own = factCounts(pkg)
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
          <ul className="flex flex-col gap-2">
            {pkg.requires.map((s) => (
              <StepFacts key={s.id} step={s} />
            ))}
          </ul>
        </Term>
      )}
      {own && (
        <Term name="Its own">
          <OwnFacts facts={pkg} of={packageName(pkg.id)} />
        </Term>
      )}
    </Facts>
  )
}

/** The account by kind and id; its values are the row's own lines. */
function AccountFacts({ account }: { account: AuthorizationAccount }) {
  return (
    <Facts>
      <Term name="Kind">
        <Mono>{account.kind}</Mono>
      </Term>
      <Term name="Id">
        <Mono>{account.id}</Mono>
      </Term>
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
      <Term name="Access">
        <Mono>webhook:{webhook.trigger}</Mono>
        <p className="text-muted-foreground">
          It delivers with its own access, so revoking the app, or its access
          ending, ends its deliveries.
        </p>
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
  webhooks,
  grantHeld,
  failure,
  technical,
}: {
  row: AuthorizationSetup
  /** The triggers the request leaves on, so a package whose webhook is one
   * of them says its code runs each time data arrives. */
  webhooks: readonly string[]
  /** On a webhook row: whether the token the app holds carries the access
   * the webhook brings. */
  grantHeld?: boolean
  /** Why the set up stopped at this row, when it did. */
  failure: string | undefined
  technical: boolean
}) {
  const title = setupTitle(row)
  const status = statusWords(row)
  const fix = consolePath(row.blocked?.fix)
  const versions = versionWords(row)
  const code = row.package && codeWords(row.package, webhooks)
  const chain = row.package ? chainLines(row.package, webhooks) : []
  const inPlace = row.action === "none"
  return (
    <li aria-label={title} className="flex gap-3 px-3 py-3">
      <RowIcon row={row} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
          <p className="font-medium [overflow-wrap:anywhere]">{title}</p>
          {status && (
            <span className="text-xs text-muted-foreground">{status}</span>
          )}
        </div>
        {versions && (
          <p className="text-xs text-muted-foreground">{versions}</p>
        )}
        {row.package?.description && (
          <p className="text-xs text-muted-foreground">
            {row.package.description}
          </p>
        )}
        {code && <p className="text-xs">{code}</p>}
        {chain.map((line) => (
          <p key={line} className="text-xs">
            {line}
          </p>
        ))}
        {row.account &&
          (inPlace ? (
            <p className="text-xs text-muted-foreground">
              Your existing account stays as it is.
            </p>
          ) : (
            <AccountValues account={row.account} />
          ))}
        {row.webhook && (
          <p className="text-xs">{grantWords(row, !!grantHeld)}</p>
        )}
        {row.blocked && (
          <div className="flex flex-col gap-0.5 text-xs">
            <p className="text-destructive">
              {asSentence(row.blocked.problem)}
            </p>
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
            {row.account && <AccountFacts account={row.account} />}
            {row.webhook && <WebhookFacts webhook={row.webhook} />}
          </div>
        )}
      </div>
    </li>
  )
}
