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
  HourglassIcon,
  PlusIcon,
  ZapIcon,
} from "lucide-react"

import { Disclosure, Mono, Term } from "@/components/authorize/parts"
import { Spinner } from "@/components/ui/spinner"
import type {
  AuthorizationAccount,
  AuthorizationPackage,
  AuthorizationPackagePublisher,
  AuthorizationPackageStep,
  AuthorizationReach,
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
  keptWords,
  networkWords,
  oauthHostWords,
  packageName,
  packagePage,
  propertyLabel,
  propertyValue,
  publishedWords,
  reachWords,
  setupTitle,
  versionWords,
  OUTSIDE_CODE_WORDS,
  runsOutsideCode,
} from "@/lib/app-setup"
import { cn } from "@/lib/utils"
import { ExternalLink } from "@/components/external-link"

const ACTION_ICONS: Partial<Record<SetupAction, typeof CheckIcon>> = {
  install: DownloadIcon,
  upgrade: ArrowUpIcon,
  create: PlusIcon,
  enable: ZapIcon,
  none: CheckIcon,
  // A waiting add-on runs nothing now; the check is for what is in place.
  wait: HourglassIcon,
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
  wait: "waits for its provider",
}

/** How far a set up got with a row. A row in place runs nothing, so a set up
 * that marks it done says nothing the row's title does not. */
function progressOf(row: AuthorizationSetup): SetupProgress | undefined {
  return row.action === "none" ? undefined : row.progress
}

function RowIcon({ row }: { row: AuthorizationSetup }) {
  const className = "mt-0.5 size-4 shrink-0"
  const progress = progressOf(row)
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
  if (row.blocked)
    return <BanIcon aria-hidden className={cn(className, "text-destructive")} />
  const Icon = ACTION_ICONS[row.action] ?? CheckIcon
  return <Icon aria-hidden className={cn(className, "text-muted-foreground")} />
}

/** The row's state beside its title: how far a set up got, that it cannot
 * run and what it waits on, or that it is in place where the title does not
 * already say so. */
function statusWords(row: AuthorizationSetup): string | undefined {
  const progress = progressOf(row)
  if (progress) return PROGRESS_WORDS[progress]
  if (row.blocked) return blockStatus(row.blocked)
  if (row.action === "none" && !row.package && !row.account && !row.webhook)
    return "In place"
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

/** Hosts in a line, each whole. */
function Hosts({ hosts }: { hosts: readonly string[] }) {
  return (
    <>
      {hosts.map((host, i) => (
        <span key={host}>
          {i > 0 && ", "}
          <Mono>{host}</Mono>
        </span>
      ))}
    </>
  )
}

/** A list of references, one per line. */
function Refs({ items }: { items: readonly string[] }) {
  return (
    <ul className="flex flex-col gap-0.5 pl-3">
      {items.map((w) => (
        <li key={w}>
          <Mono>{w}</Mono>
        </li>
      ))}
    </ul>
  )
}

/** One kind or glob a package's code reaches (decision 0215): the verb,
 * what reaches it, and the scope that covers it or would. */
function ReachLine({ reach }: { reach: AuthorizationReach }) {
  return (
    <li className="flex flex-col gap-0.5">
      <span>
        <Mono>{reach.pattern}</Mono>, {reach.write ? "written" : "read"} by{" "}
        {reach.by.map((b, i) => (
          <span key={`${b.type}/${b.id}`}>
            {i > 0 && ", "}
            {b.type} <Mono>{b.id}</Mono>
          </span>
        ))}
      </span>
      <span
        className={reach.covered ? "text-muted-foreground" : "text-destructive"}
      >
        {reach.covered ? "Covered by " : "Not covered: needs "}
        <Mono>{reach.scope}</Mono>
      </span>
    </li>
  )
}

/** A package's own kinds, functions, triggers and reach, each counted and
 * opened on demand, so a long closure is not one wall of references. */
function OwnFacts({
  facts,
  of,
}: {
  facts: Pick<
    AuthorizationPackage,
    "kinds" | "functions" | "triggers" | "reach"
  >
  of: string
}) {
  const kinds = facts.kinds ?? []
  const functions = facts.functions ?? []
  const triggers = facts.triggers ?? []
  const reach = facts.reach ?? []
  if (kinds.length + functions.length + triggers.length + reach.length === 0)
    return null
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
                {f.reads && f.reads.length > 0 ? (
                  <>
                    <span className="text-muted-foreground">May read</span>
                    <Refs items={f.reads} />
                  </>
                ) : (
                  <span className="text-muted-foreground">Reads nothing</span>
                )}
                {f.writes && f.writes.length > 0 ? (
                  <>
                    <span className="text-muted-foreground">May write</span>
                    <Refs items={f.writes} />
                  </>
                ) : (
                  <span className="text-muted-foreground">Writes nothing</span>
                )}
                <span className="text-muted-foreground">
                  Network:{" "}
                  {f.network && f.network.length > 0 ? (
                    f.network.map((host, i) => (
                      <span key={host}>
                        {i > 0 && ", "}
                        <Mono>{host}</Mono>
                      </span>
                    ))
                  ) : (
                    <span>none</span>
                  )}
                </span>
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
      {reach.length > 0 && (
        <Disclosure label="Reach" count={reach.length} of={of}>
          <ul className="flex flex-col gap-1.5">
            {reach.map((r) => (
              <ReachLine
                key={`${r.write ? "w" : "r"}:${r.pattern}`}
                reach={r}
              />
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

/** Where a document-published package comes from, as the publisher check
 * shows an app's publisher: the authority, the document's name for it, the
 * document itself, and whether DNS or a file on this server vouched for it
 * (decision 0212). */
function PublisherFacts({
  publisher,
}: {
  publisher: AuthorizationPackagePublisher
}) {
  return (
    <>
      <Term name="Published by">
        <Mono>{publisher.authority}</Mono>
        {publisher.name && <span>, {publisher.name}</span>}
      </Term>
      <Term name="Document">
        <Mono>{publisher.documentUrl}</Mono>
      </Term>
      <Term name="Read from">
        {publisher.fromFile
          ? "A local file (SUBSTRATE_INSECURE_AUTHORITY_DOCS)"
          : "DNS and the document it names"}
      </Term>
    </>
  )
}

/** "this server ships" for a package the binary offers, "its publisher
 * lists" for one an authority document does. */
function offerWords(publisher: AuthorizationPackagePublisher | undefined) {
  return publisher ? "its publisher lists" : "this server ships"
}

/** A package an install brings first: its name and what it brings, counted,
 * then its id, versions, where it comes from and its own facts. */
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
        {version(step.held)}, {offerWords(step.publisher)}{" "}
        {version(step.shipped)}
      </p>
      {step.description && (
        <p className="text-muted-foreground">{step.description}</p>
      )}
      {step.publisher && (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-0.5">
          <PublisherFacts publisher={step.publisher} />
        </dl>
      )}
      {step.blocked && (
        <p className="text-destructive">{asSentence(step.blocked.problem)}</p>
      )}
      {step.oauthHosts && step.oauthHosts.length > 0 && (
        <p>
          <span className="text-muted-foreground">OAuth hosts </span>
          <Hosts hosts={step.oauthHosts} />
        </p>
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
      {pkg.keptBy && (
        <Term name="Kept by">
          {pkg.keptBy === "publisher" ? "Its publisher" : "This repository"}
        </Term>
      )}
      <Term name="The app needs">version {pkg.required} or later</Term>
      {pkg.oauthHosts && pkg.oauthHosts.length > 0 && (
        <Term name="OAuth hosts">
          <Hosts hosts={pkg.oauthHosts} />
        </Term>
      )}
      <Term name="You hold">{version(pkg.held)}</Term>
      <Term name={pkg.publisher ? "Its publisher lists" : "This server ships"}>
        {version(pkg.shipped)}
      </Term>
      {pkg.publisher && <PublisherFacts publisher={pkg.publisher} />}
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
        <Term name="Ships">
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
  children,
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
  /** The choices the row takes: a package's provider inputs, its settings
   * and whether an add-on is included, kept inside the row they belong to. */
  children?: ReactNode
}) {
  const title = setupTitle(row)
  const status = statusWords(row)
  const fix = consolePath(row.blocked?.fix)
  const versions = publishedWords(row) ?? versionWords(row)
  const kept = keptWords(row)
  const code = row.package && codeWords(row.package, webhooks)
  const outside = row.package && runsOutsideCode(row.package)
  const network = row.package && networkWords(row.package)
  const signIn = row.package && oauthHostWords(row.package)
  const reach = row.package && reachWords(row.package)
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
        {outside && (
          <p className="text-xs">
            <span className="font-medium">Code: experimental.</span>{" "}
            {OUTSIDE_CODE_WORDS}
          </p>
        )}
        {network && <p className="text-xs">{network}</p>}
        {signIn && <p className="text-xs">{signIn}</p>}
        {reach && <p className="text-xs">{reach}</p>}
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
        {kept && row.package && (
          <p className="text-xs text-muted-foreground">
            {kept}{" "}
            <ExternalLink
              href={packagePage(row.package.id)}
              className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline"
            >
              Open its page
            </ExternalLink>
          </p>
        )}
        {row.blocked && (
          <div className="flex flex-col gap-0.5 text-xs">
            <p className="text-destructive">
              {asSentence(row.blocked.problem)}
            </p>
            {fix && (
              <ExternalLink
                href={fix}
                className="inline-flex w-fit items-center gap-1 text-primary underline-offset-4 hover:underline"
              >
                Fix this in the console
              </ExternalLink>
            )}
          </div>
        )}
        {failure && (
          <p className="text-xs text-destructive">{asSentence(failure)}</p>
        )}
        {children}
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
