/** Providers (`/providers`): every outside service the substrate uses a key
 * or a sign-in for, in three groups (decision 0233):
 *
 * - **Accounts you connect**: the shipped catalog's providers that bring
 *   data in, joined with what this repository holds of them and their
 *   accounts, one row each, opening the provider's page.
 * - **Tools for your agents**: providers whose package declares no account
 *   kind and offers functions to agents.
 * - **AI models**: one card per `llm/provider` row, with its key form in
 *   place (components/providers/model-card.tsx).
 *
 * A row says what it gives you, in one pill whether it works (On, Set up,
 * Needs attention, Paused) or offers to add it, and, only where someone
 * other than the reader could supply a key or a sign-in, whose it is. The
 * operator's daily allowance sits on the AI models header, never over the
 * providers that bring data in; a tool on the operator's key says so on its
 * own row. Where the
 * deployment loaded an operator defaults file the page opens with a welcome
 * until the reader dismisses it.
 *
 * Technical mode adds OTHER PACKAGES: every bundle this repository holds that
 * is not a provider (imported samples, the seeded llm package, anything
 * applied directly), with its version, its state, its update and its
 * removal, so nothing the old Registry listed is out of reach. */

import { PublisherDetails } from "@/components/packages/publisher"
import { useCallback, useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { ChevronRightIcon, SearchXIcon, TriangleAlertIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import { OAuthReturnNote } from "@/components/providers/oauth-return-note"
import {
  LossyUpgradeDialog,
  PendingUpgradeNotice,
  RemoveBundleButton,
  TakeButton,
  UpgradeButton,
} from "@/components/providers/bundle-actions"
import { Pill, type PillTone } from "@/components/identity/pill"
import { SectionHead } from "@/components/identity/section-head"
import { AllowanceBar } from "@/components/providers/allowance"
import { AddModelCard, ModelCard } from "@/components/providers/model-card"
import {
  ProviderLogo,
  StandingPill,
} from "@/components/providers/provider-marks"
import { ProvidersWelcome } from "@/components/providers/providers-welcome"
import {
  useProviders,
  type ProviderEntry,
} from "@/components/providers/use-providers"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  useTechnicalDetails,
  useWelcome,
} from "@/hooks/use-console-preferences"
import { useModelKeys, type ModelKeys } from "@/hooks/use-model-keys"
import { bundleState, setupCount } from "@/lib/api/bundles"
import { functionsQueryOptions } from "@/lib/api/functions"
import type { OperatorDefault, SubstrateRecord } from "@/lib/api/types"
import {
  accountKindOf,
  upgradeAvailable,
  upgradeBlocked,
  blockerWords,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import { LLM_PROVIDER_SEED } from "@/lib/llm-provider-seed"
import {
  installFailure,
  modelItem,
  modelName,
  operatorServesAny,
  packageWhoseKey,
  searchFailure,
  searchLine,
  sentenceCase,
  toolWords,
  type OperatorWords,
} from "@/lib/operator-defaults"
import {
  bundleParams,
  firstSentence,
  offeredFunctionCount,
  providerGroup,
} from "@/lib/providers"
import { packageDisplayName } from "@/lib/kind-names"
import { providersRoute } from "@/router"

const DESCRIPTION =
  "Everything your substrate connects to: the accounts that bring your data in, the tools your agents use and the AI models they run on."

const GRID = "grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5"

/** The providers are a list, one full-width row each: a description and
 * whose sign-in it uses need the width a card cannot give them. */
const LIST = "divide-y overflow-hidden rounded-[10px] border bg-background"

export function ProvidersPage() {
  const [technical] = useTechnicalDetails()
  const data = useProviders()
  const { connected, error, model } = providersRoute.useSearch()
  const navigate = useNavigate()
  const keys = useModelKeys()
  const functions = useQuery(functionsQueryOptions)
  const welcome = useWelcome()
  const items = keys.read?.items
  const words = keys.words

  const groups = useMemo(() => {
    const out: Record<"accounts" | "tools", ProviderEntry[]> = {
      accounts: [],
      tools: [],
    }
    const records = functions.data ?? []
    for (const entry of data.providers) {
      const closure = entry.row.catalog?.closure
      const group = providerGroup({
        installed: Boolean(entry.row.status?.installed),
        accountKind: Boolean(
          entry.view?.accountKind ?? accountKindOf(data.kinds, entry.row.id)
        ),
        offeredFunctions: offeredFunctionCount(records, entry.row.id),
        closureKinds: closure?.kinds?.length ?? 0,
        closureFunctions: closure?.functions?.length ?? 0,
      })
      out[group].push(entry)
    }
    return out
  }, [data.providers, data.kinds, functions.data])

  const showWelcome =
    keys.offered && Boolean(keys.read) && !welcome.busy && !welcome.dismissed

  const providerRow = (p: ProviderEntry) => (
    <ProviderRow
      key={p.row.key}
      entry={p}
      chain={data.chains(p.row)}
      technical={technical}
      items={items}
      words={words}
    />
  )

  return (
    <TablePage>
      {showWelcome ? (
        <ProvidersWelcome
          words={words}
          items={items ?? []}
          allowance={keys.read?.operator?.allowance}
          onDone={welcome.dismiss}
        />
      ) : (
        <PageHeader title="Providers" description={DESCRIPTION} />
      )}
      <OAuthReturnNote
        connected={connected}
        error={error}
        className="mt-6"
        onDismiss={() =>
          void navigate({ to: "/providers", search: {}, replace: true })
        }
      />
      <section aria-labelledby="accounts">
        <SectionHead
          id="accounts"
          title="Accounts you connect"
          hint="bring your data in and keep it up to date"
        />
        {data.error ? (
          <Empty className="rounded-[10px] border py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <SearchXIcon />
              </EmptyMedia>
              <EmptyTitle>The providers didn’t load</EmptyTitle>
              <EmptyDescription>{data.error.message}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button variant="outline" size="sm" onClick={data.refetch}>
                Try again
              </Button>
            </EmptyContent>
          </Empty>
        ) : data.pending ? (
          <div className="flex flex-col gap-1.5">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-[68px] rounded-[10px]" />
            ))}
          </div>
        ) : groups.accounts.length === 0 ? (
          <p className="text-muted-foreground">
            This substrate ships no providers that bring data in.
          </p>
        ) : (
          <ul className={LIST}>{groups.accounts.map(providerRow)}</ul>
        )}
      </section>
      {groups.tools.length > 0 && (
        <section aria-labelledby="tools">
          <SectionHead
            id="tools"
            title="Tools for your agents"
            hint="services your agents can use for you"
          />
          <ul className={LIST}>{groups.tools.map(providerRow)}</ul>
        </section>
      )}
      <ModelsGroup
        keys={keys}
        items={items}
        words={words}
        technical={technical}
        focused={model}
      />
      {technical && !data.pending && !data.error && (
        <OtherPackages
          rows={data.others}
          chains={data.chains}
          pendingUpgrades={data.pendingUpgrades}
        />
      )}
    </TablePage>
  )
}

/** The seeded rows first, in the seed's order, then the reader's own by
 * name. */
function modelOrder(rows: readonly SubstrateRecord[]): SubstrateRecord[] {
  const seeded = Object.keys(LLM_PROVIDER_SEED)
  const rank = (id: string) => {
    const at = seeded.indexOf(id)
    return at < 0 ? seeded.length : at
  }
  return [...rows].sort(
    (a, b) =>
      rank(a.id) - rank(b.id) || modelName(a).localeCompare(modelName(b))
  )
}

function ModelsGroup({
  keys,
  items,
  words,
  technical,
  focused,
}: {
  keys: ModelKeys
  items: readonly OperatorDefault[] | undefined
  words: OperatorWords
  technical: boolean
  focused?: string
}) {
  const allowance = keys.read?.operator?.allowance
  const rows = keys.rows ? modelOrder(keys.rows) : undefined
  const removed = rows
    ? Object.keys(LLM_PROVIDER_SEED).filter(
        (id) => !rows.some((r) => r.id === id)
      )
    : []
  let body: ReactNode
  const unchecked = keys.readError && (
    <div
      role="alert"
      className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-[10px] border px-3.5 py-2.5 text-[13px]"
    >
      <span className="min-w-0 flex-1">
        Couldn’t check whose key each AI model uses: {keys.readError.message}
      </span>
      <Button variant="outline" size="sm" onClick={keys.recheck}>
        Try again
      </Button>
    </div>
  )
  if (keys.error) {
    body = (
      <p role="alert" className="text-destructive">
        The AI models didn’t load: {keys.error.message}
      </p>
    )
  } else if (!rows) {
    body = (
      <div className={GRID}>
        {Array.from({ length: 3 }, (_, i) => (
          <Skeleton key={i} className="h-[118px] rounded-[10px]" />
        ))}
      </div>
    )
  } else {
    body = (
      <div className={GRID}>
        {rows.map((row) => (
          <ModelCard
            key={row.id}
            row={row}
            modelKey={keys.keyOf(row)}
            words={words}
            search={searchLine(items, row.id, words)}
            searchFailed={searchFailure(items, row.id)}
            technical={technical}
            focused={focused === row.id}
          />
        ))}
        {removed.map((id) => (
          <AddModelCard
            key={id}
            id={id}
            words={words}
            operatorOffers={Boolean(modelItem(items, id)?.operatorOffers)}
            technical={technical}
          />
        ))}
      </div>
    )
  }
  return (
    <section aria-labelledby="ai-models">
      <SectionHead
        id="ai-models"
        title="AI models"
        hint="what your agents think with"
      />
      {allowance && operatorServesAny(items) && (
        <AllowanceBar allowance={allowance} words={words} />
      )}
      {unchecked}
      {body}
    </section>
  )
}

const LINE_TONE: Record<ProviderEntry["standing"]["tone"], string> = {
  on: "text-muted-foreground",
  setup: "text-warning",
  attention: "text-destructive",
  paused: "text-muted-foreground",
  add: "text-faint",
}

function ProviderRow({
  entry,
  chain,
  technical,
  items,
  words,
}: {
  entry: ProviderEntry
  chain: RequirementNode[]
  technical: boolean
  items: readonly OperatorDefault[] | undefined
  words: OperatorWords
}) {
  const { row, info, standing } = entry
  const whose = packageWhoseKey(items, row.id, info.name, words)
  const failed = installFailure(items, row.id)
  // A tool is described by what it does, in the welcome's words.
  const purpose = toolWords(row.id)
  const publisher = row.catalog?.publisher
  return (
    <li
      data-slot="provider-card"
      className="group relative flex cursor-pointer items-start gap-3 px-4 py-3 transition-colors hover:bg-hover"
    >
      <ProviderLogo info={info} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <Link
            to="/providers/$authority/$pkg"
            params={bundleParams(row.id)}
            className="font-semibold outline-none after:absolute after:inset-0 focus-visible:after:ring-2 focus-visible:after:ring-ring/50 focus-visible:after:ring-inset"
          >
            {info.name}
          </Link>
          {publisher && !technical && (
            <span className="text-[12px] [overflow-wrap:anywhere] text-faint">
              by{" "}
              {publisher.name
                ? `${publisher.name} · ${publisher.authority}`
                : publisher.authority}
            </span>
          )}
        </div>
        {(purpose || row.catalog?.description) && (
          <p className="text-[13px] text-muted-foreground">
            {purpose
              ? `${sentenceCase(purpose)} for your agents.`
              : firstSentence(row.catalog?.description)}
          </p>
        )}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px]">
          <span className={LINE_TONE[standing.tone]}>{standing.line}</span>
          {entry.updateAvailable && (
            <Pill tone="accent" dot={false}>
              Update available
            </Pill>
          )}
        </div>
        {whose && <p className="text-[12.5px]">{whose.line}</p>}
        {whose?.notice && (
          <p className="-mt-0.5 text-[12px] text-faint">{whose.notice}</p>
        )}
        {failed !== undefined && (
          <p className="text-[12.5px] text-destructive">
            {words.Name} couldn’t add it for you
            {failed ? `: ${failed}` : "."}
          </p>
        )}
        {technical && publisher && (
          <div className="relative z-10 mt-1 cursor-auto">
            <PublisherDetails publisher={publisher} />
          </div>
        )}
        {technical && <IdText value={row.id} />}
      </div>
      <div className="flex shrink-0 items-center gap-1.5 self-center">
        {standing.tone === "add" ? (
          <TakeButton
            row={row}
            chain={chain}
            name={info.name}
            label="Add"
            className="relative z-10 h-7"
          />
        ) : (
          <StandingPill standing={standing} />
        )}
        <ChevronRightIcon
          aria-hidden
          className="size-4 text-faint transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground"
        />
      </div>
    </li>
  )
}

// ── other packages (technical) ──────────────────────────────────────────────

const STATE_WORDS: Record<
  ReturnType<typeof bundleState>,
  { word: string; tone: PillTone }
> = {
  enabled: { word: "On", tone: "ok" },
  disabled: { word: "Paused", tone: "neutral" },
  uninstalled: { word: "Removed", tone: "neutral" },
  quarantined: { word: "Failed to load", tone: "bad" },
}

function tierWord(row: BundleRow): string {
  if (row.tier === "sample") return "Sample"
  if (row.tier === "provider") return "Provider"
  return "Added by hand"
}

function OtherPackages({
  rows,
  chains,
  pendingUpgrades,
}: {
  rows: BundleRow[]
  chains: (row: BundleRow) => RequirementNode[]
  pendingUpgrades: ReturnType<typeof useProviders>["pendingUpgrades"]
}) {
  // The row whose lossy update is being confirmed, by key: the dialog reads
  // that row's current preview, so a refetch shows the fresh plan.
  const [lossyID, setLossyID] = useState<string | null>(null)
  const confirmLoss = useCallback((row: BundleRow) => setLossyID(row.key), [])
  const closeLoss = useCallback(() => setLossyID(null), [])
  return (
    <section aria-labelledby="other-packages">
      <SectionHead
        id="other-packages"
        title="Other packages"
        hint="Samples you added, packages added by hand, and the substrate’s own."
      />
      {pendingUpgrades.length > 0 && (
        <div className="mb-3 flex flex-col gap-2">
          {pendingUpgrades.map((item) => (
            <PendingUpgradeNotice key={item.package} item={item} />
          ))}
        </div>
      )}
      {rows.length === 0 ? (
        <p className="text-muted-foreground">
          This repository holds no other packages.
        </p>
      ) : (
        <div className="overflow-x-auto rounded-[10px] border">
          <table className="w-full min-w-[720px] text-[13px]">
            <thead>
              <tr className="border-b text-left text-xs text-faint">
                <th className="w-[30%] px-3 py-2 font-medium">Package</th>
                <th className="w-[26%] px-3 py-2 font-medium">From</th>
                <th className="px-3 py-2 text-right font-medium">Version</th>
                <th className="px-3 py-2 font-medium">State</th>
                <th className="px-3 py-2 font-medium">Update</th>
                <th className="px-3 py-2">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <OtherPackageRow
                  key={row.key}
                  row={row}
                  chain={chains(row)}
                  onConfirmLoss={confirmLoss}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      <LossyUpgradeDialog
        row={rows.find((r) => r.key === lossyID)}
        name={packageDisplayName(
          rows.find((r) => r.key === lossyID)?.package ?? ""
        )}
        onClose={closeLoss}
      />
    </section>
  )
}

function OtherPackageRow({
  row,
  chain,
  onConfirmLoss,
}: {
  row: BundleRow
  chain: RequirementNode[]
  onConfirmLoss: (row: BundleRow) => void
}) {
  const status = row.status!
  const state = STATE_WORDS[bundleState(status)]
  const setup = setupCount(status)
  const name = packageDisplayName(row.package || row.name)
  return (
    <tr
      data-slot="other-package"
      className="border-b align-top last:border-0 hover:bg-hover"
    >
      <td className="px-3 py-2.5">
        <Link
          to="/providers/$authority/$pkg"
          params={bundleParams(row.id)}
          className="font-medium underline-offset-2 hover:underline"
        >
          {name}
        </Link>
        <div>
          <IdText value={row.id} copy />
        </div>
        {status.quarantined && status.quarantineReason && (
          <p className="mt-1 max-w-[60ch] text-xs text-destructive">
            {status.quarantineReason}
          </p>
        )}
      </td>
      <td className="px-3 py-2.5 text-muted-foreground">
        {tierWord(row)}
        {status.origin && (
          <div>
            <IdText value={status.origin} />
            {status.modified && (
              <span className="ml-1 text-xs text-faint">· edited since</span>
            )}
          </div>
        )}
      </td>
      <td className="px-3 py-2.5 text-right text-muted-foreground tabular-nums">
        {status.version ?? "—"}
      </td>
      <td className="px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-1.5">
          <Pill tone={state.tone}>{state.word}</Pill>
          {setup > 0 && (
            <Pill tone="warn" dot={false}>
              {setup === 1 ? "1 thing to set up" : `${setup} things to set up`}
            </Pill>
          )}
        </div>
      </td>
      <td className="px-3 py-2.5">
        {upgradeBlocked(row) ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  className="inline-flex cursor-help rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                />
              }
            >
              <Pill tone="warn" dot={false}>
                <TriangleAlertIcon aria-hidden className="size-3" />
                Update blocked
              </Pill>
            </TooltipTrigger>
            <TooltipContent className="max-w-96">
              <div className="space-y-1">
                {(row.upgrade?.blockers ?? []).map((b) => (
                  <p key={b}>{blockerWords(b)}</p>
                ))}
              </div>
            </TooltipContent>
          </Tooltip>
        ) : upgradeAvailable(row) ? (
          <UpgradeButton row={row} name={name} onConfirmLoss={onConfirmLoss} />
        ) : (
          <span className="text-faint">Current</span>
        )}
      </td>
      <td className="px-3 py-2.5 text-right">
        {status.quarantined && row.catalog ? (
          <TakeButton row={row} chain={chain} name={name} label="Add again" />
        ) : (
          <RemoveBundleButton bundle={status} name={name} variant="ghost" />
        )}
      </td>
    </tr>
  )
}
