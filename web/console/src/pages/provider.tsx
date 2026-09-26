/** One provider (`/providers/$authority/$pkg`; a provider is a bundle, and
 * a bundle's id is its package). The page a person finishes setting a
 * provider up on and comes back to: its state, the four set-up steps with
 * the current one's action, its accounts with their sync health, what it
 * brings in, its tools, what it recently changed, and its own settings. Pause and Remove sit in the
 * header, each confirmed in plain words. Technical mode adds the bundle id
 * and version, which record each declared need uses, and the connection
 * details (triggers, runs, parked deliveries).
 *
 * The same address serves any other package this repository holds (an
 * imported sample, the seeded llm package): the Providers list links its
 * Other packages here, so a package's settings, update and removal have a
 * home. Such a page has no set-up steps and no accounts. */

import { useCallback, useEffect, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  CheckIcon,
  PackageIcon,
  SearchXIcon,
  TriangleAlertIcon,
} from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { PageHeader } from "@/components/identity/page-header"
import { DocPage } from "@/components/identity/page-layout"
import { AccountsSection } from "@/components/providers/accounts-section"
import {
  ImportAgainNote,
  LossyUpgradeDialog,
  PauseBundleButton,
  RemoveBundleButton,
  TakeButton,
  UpgradeBlockedNote,
  UpgradeButton,
} from "@/components/providers/bundle-actions"
import { ConnectionDetails } from "@/components/providers/connection-details"
import { OAuthReturnNote } from "@/components/providers/oauth-return-note"
import { RecentActivity } from "@/components/providers/provider-activity"
import { ProviderProblems } from "@/components/providers/provider-problems"
import {
  BringsIn,
  ProviderTools,
} from "@/components/providers/provider-contents"
import { Pill } from "@/components/identity/pill"
import { SectionHead } from "@/components/identity/section-head"
import {
  ProviderLogo,
  StandingPill,
} from "@/components/providers/provider-marks"
import {
  InputCard,
  SettingsForm,
  SetupItemRow,
} from "@/components/providers/settings-section"
import { SetupSteps } from "@/components/providers/setup-steps"
import {
  useProviders,
  type ProviderEntry,
  type ProvidersData,
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
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { bundleState } from "@/lib/api/bundles"
import { settingRecordsQueryOptions } from "@/lib/api/settings"
import type { BundleStatus, KindInfo } from "@/lib/api/types"
import {
  isInputSetupCode,
  isSettingSetupCode,
  missingChain,
  upgradeAvailable,
  upgradeBlocked,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import { currentStep } from "@/lib/providers"
import { groupSettings, type SettingField } from "@/lib/settings"
import { cn } from "@/lib/utils"
import { providerRoute } from "@/router"
import { packageDisplayName } from "@/lib/kind-names"

export function ProviderPage() {
  const { authority, pkg } = providerRoute.useParams()
  const { account, connected, error } = providerRoute.useSearch()
  const navigate = useNavigate()
  const id = `${authority}/${pkg}`
  const oauthReturn = (
    <OAuthReturnNote
      connected={connected}
      error={error}
      className="mt-6"
      onDismiss={() =>
        void navigate({
          to: "/providers/$authority/$pkg",
          params: { authority, pkg },
          search: { account },
          replace: true,
        })
      }
    />
  )
  const data = useProviders()
  const settings = useQuery(settingRecordsQueryOptions)
  const settingFields = useMemo(
    () =>
      groupSettings(settings.data ?? []).find((g) => g.bundle === id)?.fields ??
      [],
    [settings.data, id]
  )

  // An address with a hash (`#settings`, where a fresh import with an empty
  // setting is sent) scrolls there once the section exists.
  const ready = !data.pending && !settings.isPending
  useEffect(() => {
    if (!ready || !window.location.hash) return
    document
      .getElementById(decodeURIComponent(window.location.hash.slice(1)))
      ?.scrollIntoView?.({ block: "start" })
  }, [ready])

  if (data.pending) return <ProviderSkeleton />
  if (data.error) {
    return (
      <DocPage>
        <Empty className="rounded-[10px] border py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>This page didn’t load</EmptyTitle>
            <EmptyDescription>{data.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" size="sm" onClick={data.refetch}>
              Try again
            </Button>
          </EmptyContent>
        </Empty>
      </DocPage>
    )
  }

  const entry = data.providers.find((p) => p.row.id === id)
  const row = entry?.row ?? data.rows.find((r) => r.id === id && r.status)
  if (!row) {
    return (
      <DocPage>
        <Empty className="rounded-[10px] border py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>Nothing here</EmptyTitle>
            <EmptyDescription>
              No provider or package is called <IdText value={id} />.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              render={<Link to="/providers" />}
            >
              See all providers
            </Button>
          </EmptyContent>
        </Empty>
      </DocPage>
    )
  }
  return entry ? (
    <ProviderDoc
      entry={entry}
      data={data}
      settingFields={settingFields}
      highlight={account}
      notice={oauthReturn}
    />
  ) : (
    <PackageDoc row={row} data={data} settingFields={settingFields} />
  )
}

/** The update control a header carries: offered, stated as blocked below
 * the header, or nothing. */
function useLossy() {
  const [lossy, setLossy] = useState(false)
  const open = useCallback(() => setLossy(true), [])
  const close = useCallback(() => setLossy(false), [])
  return { lossy, open, close }
}

function ProviderDoc({
  entry,
  data,
  settingFields,
  highlight,
  notice,
}: {
  entry: ProviderEntry
  data: ProvidersData
  settingFields: SettingField[]
  highlight?: string
  /** What an account's connect came back with, above everything else. */
  notice?: React.ReactNode
}) {
  const [technical] = useTechnicalDetails()
  const { row, info, view, standing, steps } = entry
  const status = row.status
  const installed = Boolean(status?.installed)
  const chain = data.chains(row)
  const lossy = useLossy()
  const now = currentStep(steps)
  const accounts = view?.accounts ?? []

  return (
    <DocPage>
      <PageHeader
        glyph={<ProviderLogo info={info} size="lg" />}
        title={info.name}
        meta={
          <>
            {standing.pill ? (
              <StandingPill standing={standing} />
            ) : (
              <Pill tone="neutral">Not added</Pill>
            )}
            {entry.updateAvailable && (
              <Pill tone="accent" dot={false}>
                Update available
              </Pill>
            )}
            {technical && <IdText value={row.id} copy />}
            {technical && status?.version !== undefined && (
              <span>version {status.version}</span>
            )}
          </>
        }
        description={row.catalog?.description}
        actions={
          <HeaderActions
            row={row}
            status={status}
            name={info.name}
            onConfirmLoss={lossy.open}
          />
        }
      />
      <LossyUpgradeDialog
        row={lossy.lossy ? row : undefined}
        name={info.name}
        onClose={lossy.close}
      />

      {notice}
      <ProviderProblems entry={entry} chain={chain} className="mt-6" />

      <SectionHead
        id="setup"
        title="Set up"
        hint={
          now
            ? `Step ${now.n} of ${steps.length}`
            : "Done. It keeps your copy up to date on its own."
        }
      />
      {missingChain(chain).length > 0 && !installed && (
        <p className="mb-2 text-[12.5px] text-muted-foreground">
          Adding it also adds the{" "}
          {missingChain(chain).length === 1 ? "package" : "packages"} it needs
          first.
        </p>
      )}
      <SetupSteps entry={entry} chain={chain} />

      {view && accounts.length > 0 && (
        <>
          <SectionHead
            id="accounts"
            title="Accounts"
            hint={`${accounts.length} ${accounts.length === 1 ? "account" : "accounts"}`}
          />
          <AccountsSection
            view={view}
            providerName={info.name}
            highlight={highlight}
          />
          {data.accountsRead.data?.capped && (
            <p className="mt-2 text-[12.5px] text-muted-foreground">
              Showing the first accounts only.
            </p>
          )}
        </>
      )}

      <BringsIn
        bundleId={row.id}
        kinds={data.kinds}
        catalog={row.catalog}
        installed={installed}
      />
      <ProviderTools catalog={row.catalog} installed={installed} />
      {installed && <RecentActivity bundleId={row.id} name={info.name} />}

      {status && (
        <BundleSettings
          status={status}
          kinds={data.kinds}
          settingFields={settingFields}
          technical={technical}
          // The credentials input is step 2's; everything else is here.
          hideInput={
            view?.configKind
              ? status.inputs?.find((i) => i.kind === view.configKind?.identity)
                  ?.name
              : undefined
          }
        />
      )}

      {technical && accounts.length > 0 && (
        <>
          <SectionHead
            title="Connection details"
            hint="Each account’s sync, the triggers that run it, and what they parked"
          />
          <ConnectionDetails accounts={accounts} />
        </>
      )}
      {technical && <Requirements chain={chain} />}
    </DocPage>
  )
}

/** Any other package this repository holds, on the same address. */
function PackageDoc({
  row,
  data,
  settingFields,
}: {
  row: BundleRow
  data: ProvidersData
  settingFields: SettingField[]
}) {
  const [technical] = useTechnicalDetails()
  const status = row.status!
  const name = packageDisplayName(row.package || row.name)
  const lossy = useLossy()
  const state = bundleState(status)
  return (
    <DocPage>
      <PageHeader
        glyph={
          <span className="grid size-11 place-items-center rounded-[10px] border bg-panel text-muted-foreground">
            <PackageIcon aria-hidden className="size-5" />
          </span>
        }
        title={name}
        meta={
          <>
            <Pill
              tone={
                state === "enabled"
                  ? "ok"
                  : state === "quarantined"
                    ? "bad"
                    : "neutral"
              }
            >
              {state === "enabled"
                ? "On"
                : state === "quarantined"
                  ? "Failed to load"
                  : "Paused"}
            </Pill>
            <span>
              {row.tier === "sample"
                ? "A sample you added"
                : row.tier === "provider"
                  ? "A provider"
                  : "Added by hand"}
            </span>
            {technical && <IdText value={row.id} copy />}
            {technical && status.version !== undefined && (
              <span>version {status.version}</span>
            )}
            {technical && status.origin && (
              <span>
                from <IdText value={status.origin} />
                {status.originVersion
                  ? ` at version ${status.originVersion}`
                  : ""}
                {status.modified ? ", edited since" : ""}
              </span>
            )}
          </>
        }
        description={row.catalog?.description}
        actions={
          <HeaderActions
            row={row}
            status={status}
            name={name}
            onConfirmLoss={lossy.open}
          />
        }
      />
      <LossyUpgradeDialog
        row={lossy.lossy ? row : undefined}
        name={name}
        onClose={lossy.close}
      />
      {status.quarantined && (
        <Callout className="mt-6">
          <p className="font-medium">{name} failed to load.</p>
          {status.quarantineReason && (
            <p className="text-muted-foreground">{status.quarantineReason}</p>
          )}
          {row.catalog && (
            <div className="mt-1">
              <TakeButton
                row={row}
                chain={data.chains(row)}
                name={name}
                label="Add it again"
              />
            </div>
          )}
        </Callout>
      )}
      {upgradeBlocked(row) && <UpgradeBlockedNote row={row} className="mt-6" />}
      {row.catalog && (
        <div className="mt-6 empty:hidden">
          <ImportAgainNote item={row.catalog} />
        </div>
      )}
      <BundleSettings
        status={status}
        kinds={data.kinds}
        settingFields={settingFields}
        technical={technical}
      />
      <BringsIn
        bundleId={row.id}
        kinds={data.kinds}
        catalog={row.catalog}
        installed={status.installed}
        title="What it adds"
      />
      <ProviderTools catalog={row.catalog} installed={status.installed} />
      <Requirements chain={data.chains(row)} />
    </DocPage>
  )
}

function HeaderActions({
  row,
  status,
  name,
  onConfirmLoss,
}: {
  row: BundleRow
  status: BundleStatus | undefined
  name: string
  onConfirmLoss: () => void
}) {
  if (!status) return null
  return (
    <>
      {status.installed && upgradeAvailable(row) && !upgradeBlocked(row) && (
        <UpgradeButton row={row} name={name} onConfirmLoss={onConfirmLoss} />
      )}
      <PauseBundleButton bundle={status} name={name} />
      <RemoveBundleButton bundle={status} name={name} />
    </>
  )
}

/** The bundle's settings, what else it still needs, and (technical mode)
 * which record each declared need uses. Renders nothing for a bundle that
 * has none of the three. */
function BundleSettings({
  status,
  kinds,
  settingFields,
  technical,
  hideInput,
}: {
  status: BundleStatus
  kinds: KindInfo[]
  settingFields: SettingField[]
  technical: boolean
  hideInput?: string
}) {
  // A setting item is what the form marks on its field; an input's own
  // problem is its card's (technical) or step 2's.
  const standalone = (status.setup ?? []).filter(
    (item) =>
      !isSettingSetupCode(item.code) &&
      !isInputSetupCode(item.code) &&
      !(item.code === "oauth-client" && hideInput)
  )
  const inputs = (status.inputs ?? []).filter((i) => i.name !== hideInput)
  const unresolved = inputs.filter((i) => !i.record)
  const showInputs = technical && inputs.length > 0
  if (
    !settingFields.length &&
    !standalone.length &&
    !showInputs &&
    !unresolved.length
  )
    return null
  return (
    <section aria-labelledby="settings">
      <SectionHead
        id="settings"
        title="Settings"
        hint={
          settingFields.length
            ? "What it needs from you to run"
            : "What it still needs before it runs"
        }
      />
      <div className="flex flex-col gap-3">
        {standalone.map((item, i) => (
          <SetupItemRow
            key={`${item.code}:${item.input ?? item.record ?? i}`}
            item={item}
            kinds={kinds}
          />
        ))}
        {!technical &&
          unresolved.map((input) => (
            <SetupItemRow
              key={input.name}
              item={
                status.setup?.find((s) => s.input === input.name) ?? {
                  code: "missing",
                  message: `It needs ${input.description ?? input.name}.`,
                }
              }
              kinds={kinds}
            />
          ))}
        {settingFields.length > 0 && <SettingsForm fields={settingFields} />}
        {showInputs && (
          <>
            <p className="mt-2 text-[12.5px] text-muted-foreground">
              Each thing it needs uses one record: the one you pick here, else
              the one named <IdText value="default" />, else the only one there
              is.
            </p>
            {inputs.map((input) => (
              <InputCard
                key={input.name}
                bundle={status}
                input={input}
                kinds={kinds}
              />
            ))}
          </>
        )}
      </div>
    </section>
  )
}

/** The packages this one declares against, nested, each here or missing
 * (and at which version, where it is held below the floor). */
function Requirements({ chain }: { chain: RequirementNode[] }) {
  if (!chain.length) return null
  return (
    <section aria-labelledby="requires">
      <SectionHead
        id="requires"
        title="Needs these packages"
        hint="Everything it points at must be here first"
      />
      <RequirementTree nodes={chain} />
    </section>
  )
}

function RequirementTree({ nodes }: { nodes: RequirementNode[] }) {
  return (
    <ul className="flex flex-col gap-1 text-[13px]">
      {nodes.map((node) => (
        <li key={node.package} data-present={node.present}>
          <span
            className={cn(
              "inline-flex flex-wrap items-center gap-1.5",
              node.present ? "text-muted-foreground" : "text-warning"
            )}
          >
            {node.present ? (
              <CheckIcon aria-hidden className="size-3.5 shrink-0 text-ok" />
            ) : (
              <TriangleAlertIcon aria-hidden className="size-3.5 shrink-0" />
            )}
            <IdText value={node.package} />
            <span>
              {node.present
                ? "here"
                : node.held !== undefined
                  ? `here at version ${node.held}; needs version ${node.atLeast} or later`
                  : "not here yet"}
            </span>
          </span>
          {node.requires.length > 0 && (
            <div className="mt-1 border-l pl-3">
              <RequirementTree nodes={node.requires} />
            </div>
          )}
        </li>
      ))}
    </ul>
  )
}

function Callout({
  children,
  className,
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div
      role="note"
      className={cn(
        "flex items-start gap-2.5 rounded-lg bg-bad-soft px-3.5 py-3 text-[13px]",
        className
      )}
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-destructive"
      />
      <div className="min-w-0 space-y-0.5">{children}</div>
    </div>
  )
}

function ProviderSkeleton() {
  return (
    <DocPage>
      <div className="flex items-start gap-3.5">
        <Skeleton className="size-11 rounded-[10px]" />
        <div className="flex-1">
          <Skeleton className="h-7 w-40" />
          <Skeleton className="mt-2 h-4 w-72" />
        </div>
      </div>
      <Skeleton className="mt-10 h-5 w-24" />
      <Skeleton className="mt-3 h-56 w-full rounded-[10px]" />
    </DocPage>
  )
}
