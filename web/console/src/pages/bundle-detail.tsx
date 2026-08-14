/** Bundle detail (`/registry/:id`, id = the bundle's owned authority): the
 * runtime state up top with the lifecycle verbs — disable/enable is reversible
 * and keeps data + schema; purge tombstones the data (refused while live, so
 * disable first); uninstall tears down the schema and callables (refused while
 * data lives, so purge first) — each behind a confirm that states the posture,
 * then the setup/inputs surface. `bundle` stays the schema term; the
 * advanced/record link still exposes the bundle authority.
 *
 * The same closure facts the Registry row discloses before an import are here
 * after it: the catalog facet (Vocabulary / Integration), the shipped version,
 * the authorities the closure REQUIRES — each marked present or missing against
 * the live kind registry — and the kinds it installed, linked to their
 * collections.
 *
 * Provider-copy gating: the OAuth callback URL and the Accounts/Connect
 * section render ONLY when the bundle declares provider interfaces
 * (declaresProviderInterfaces: an accountconfig account kind in its owned
 * authority, or a declared input whose kind carries the oauth2 trait).
 *
 * Setup is the input surface, and it renders only when the bundle declares
 * inputs or the status carries setup items. A bundle with neither (every
 * vocabulary bundle) shows nothing there: no heading, no empty state. The
 * lifecycle badge and the setup chip are SEPARATE signals and are never
 * collapsed into one. */

import { Fragment, useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import {
  ArrowUpRightIcon,
  BotIcon,
  BoxesIcon,
  BoxIcon,
  CheckIcon,
  CopyIcon,
  FunctionSquareIcon,
  PencilIcon,
  PlugZapIcon,
  PlusIcon,
  SearchXIcon,
  TriangleAlertIcon,
  ZapIcon,
} from "lucide-react"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { BundleStateBadge, SetupBadge } from "@/components/bundle-state-badge"
import { RecordConfigForm } from "@/components/record-config-form"
import {
  ACCOUNT_CONFIG_TRAIT,
  bindBundleInput,
  bundleState,
  bundleStatusQueryOptions,
  parseSubstrateOAuthMessage,
  purgeBundle,
  refetchBundleStateSoon,
  runBundleVerb,
  uninstallBundle,
  seedBundleStatus,
  setupCount,
  startOAuth,
  traitRecordsQueryOptions,
  type BundleStatus,
  type BundleVerb,
  type InputStatus,
  type SetupItem,
} from "@/lib/api/bundles"
import {
  catalogItemQueryOptions,
  oauthCallbackURL,
  type CatalogItem,
} from "@/lib/api/catalog"
import {
  recordsQueryOptions,
  recordCountQueryOptions,
  formatCount,
} from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { CORE_AUTHORITY } from "@/lib/api/http"
import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  accountKindOf,
  bundleRecordRows,
  declaresProviderInterfaces,
  installedKindRows,
  isInputSetupCode,
  oauthConnectBlocked,
  presentAuthorities,
  requirementsOf,
  type Requirement,
  type ShippedRecordRow,
  type KindRow,
} from "@/lib/bundles"
import { cellValue, recordTitle } from "@/lib/format"
import { splitKind, kindByIdentity } from "@/lib/definition"
import { cn } from "@/lib/utils"
import { bundleDetailRoute } from "@/router"

/** Google's own "create an OAuth client" documentation — where the owner sets
 * up the client and registers the redirect URI below. */
const GOOGLE_OAUTH_DOCS =
  "https://support.google.com/cloud/answer/6158849"

/** The provider callback URL, read-only with a copy affordance — the value the
 * owner must register in their OAuth client. Provider-specific: it renders only
 * on an integration bundle (declaresProviderInterfaces). */
function CallbackUrlNote() {
  const url = oauthCallbackURL()
  const [copied, setCopied] = useState(false)
  return (
    <div className="rounded-md border bg-muted/30 px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">OAuth callback URL</span>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 gap-1 px-2 text-xs"
          onClick={() => {
            void navigator.clipboard?.writeText(url)
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          }}
        >
          {copied ? <CheckIcon className="size-3" /> : <CopyIcon className="size-3" />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <p className="mt-1 data text-xs break-all text-muted-foreground">{url}</p>
      <p className="mt-1.5 text-xs text-muted-foreground">
        Add this redirect URI to your OAuth client.{" "}
        <a
          href={GOOGLE_OAUTH_DOCS}
          target="_blank"
          rel="noreferrer"
          className="underline-offset-4 hover:underline"
        >
          Google's guide
          <ArrowUpRightIcon className="inline size-3 align-text-top" />
        </a>
      </p>
    </div>
  )
}

/** The authorities the closure declares against, each marked against the LIVE
 * kind registry — the same check admission makes (`schema.resolveBundle`), so a
 * requirement torn down after the import reads as missing here rather than
 * silently rotting. Renders only when the shipped closure names any. */
function RequiresNote({ requirements }: { requirements: Requirement[] }) {
  const missing = requirements.filter((r) => !r.present)
  return (
    <div className="rounded-md border bg-muted/30 px-4 py-3">
      <span className="text-xs font-medium">Requires</span>
      <div className="flex flex-wrap gap-1.5 pt-1.5">
        {requirements.map((req) => (
          <span
            key={req.authority}
            className={cn(
              "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 data text-xs",
              req.present
                ? "bg-background text-muted-foreground"
                : "border-warning/40 text-warning"
            )}
            title={
              req.present
                ? `${req.authority} is imported`
                : `${req.authority} is not imported`
            }
          >
            {req.present ? (
              <CheckIcon className="size-3 shrink-0" />
            ) : (
              <TriangleAlertIcon className="size-3 shrink-0" />
            )}
            {req.authority}
            <span className="sr-only">
              {req.present ? " imported" : " missing"}
            </span>
          </span>
        ))}
      </div>
      <p className="pt-1.5 text-xs text-muted-foreground">
        {missing.length
          ? `Not in this repository: ${missing
              .map((r) => r.authority)
              .join(", ")}. This bundle's mappings and edges point at it — re-import that authority's bundle from the registry.`
          : "The vocabulary this bundle's mappings, edges and trigger subscriptions point at. All of it is imported."}
      </p>
    </div>
  )
}

// ── the lifecycle verbs ─────────────────────────────────────────────────────

interface VerbPlan {
  /** The button label. */
  label: string
  /** The success-toast copy — the verb's own past tense, spelled out (a
   * mechanical `label + "d"` writes "Uninstalld"). */
  done: string
  /** The confirm-dialog copy: what happens and how reversible it is. */
  title: string
  body: React.ReactNode
  /** Destructive verbs wear the destructive button. */
  destructive?: boolean
  /** The lifecycle verbs answer with the fresh BundleStatus (seeded into the
   * caches on success); purge answers with a count, so it yields null. */
  run: (id: string) => Promise<BundleStatus | null>
}

function verbPlan(verb: BundleVerb | "purge", b: BundleStatus): VerbPlan {
  switch (verb) {
    case "disable":
      return {
        label: "Disable",
        done: "Disabled.",
        title: `Disable ${b.name}?`,
        body: "Execution stops — triggers stop delivering and callables stop resolving. The schema and data stay exactly as they are; enable brings it back with the cursors intact.",
        run: (id) => runBundleVerb(id, "disable"),
      }
    case "enable":
      return {
        label: "Enable",
        done: "Enabled.",
        title: `Enable ${b.name}?`,
        body: "Execution resumes from where the cursors stand.",
        run: (id) => runBundleVerb(id, "enable"),
      }
    case "uninstall":
      return {
        label: "Uninstall",
        done: "Uninstalled.",
        title: `Uninstall ${b.name}?`,
        body: "Tears down the schema, callables and runtime registration for good. Refused while live data remains — purge the data first. Reinstalling means re-applying the closure.",
        destructive: true,
        run: (id) => uninstallBundle(id).then(() => null),
      }
    case "purge":
      return {
        label: "Purge",
        done: "Purged.",
        title: `Purge ${b.name}'s data?`,
        body: (
          <>
            Tombstones every live row in{" "}
            <span className="data">{b.authority}</span> —{" "}
            <span className="data">{(b.liveRecords ?? 0).toLocaleString()}</span>{" "}
            {(b.liveRecords ?? 0) === 1 ? "record" : "records"} — through the
            finalizer flow. Refused while the bundle is running: disable it
            first. This is not reversible, and it must run before uninstall.
          </>
        ),
        destructive: true,
        run: async (id) => {
          await purgeBundle(id)
          return null
        },
      }
  }
}

function LifecycleButtons({ bundle }: { bundle: BundleStatus }) {
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState<VerbPlan | null>(null)

  const mutation = useMutation({
    mutationFn: (plan: VerbPlan) => plan.run(bundle.id),
    onSuccess: (data, plan) => {
      setConfirming(null)
      toast.add({ type: "success", title: plan.done })
      // The verb's response is the freshest status there is — seed it so this
      // page and the bundles list flip immediately.
      if (data) seedBundleStatus(queryClient, data)
      // The verb moves runtime state the whole console reads (status, counts,
      // the sidebar). Drop everything and re-read — and re-read the bundle
      // surfaces again shortly, since the probe-backed reads can lag the verb.
      void queryClient.invalidateQueries()
      refetchBundleStateSoon(queryClient)
    },
    onError: (error, plan) => {
      setConfirming(null)
      toast.add({
        type: "error",
        title: `Could not ${plan.label.toLowerCase()} the bundle`,
        description: error.message,
      })
      void queryClient.invalidateQueries()
    },
  })

  // The verbs follow the lifecycle order exactly: disable → purge → uninstall.
  // Enable/disable toggle a marker on the RUNTIME registration, so they exist
  // ONLY while installed. Purge is refused while running, so it only offers
  // once disabled; uninstall is refused while data lives, so it only offers
  // once the group is empty. The console never shows a verb the server would
  // refuse — it walks the owner down the ladder one rung at a time.
  const verbs: (BundleVerb | "purge")[] = []
  if (bundle.installed) {
    verbs.push(bundle.enabled ? "disable" : "enable")
    if (!bundle.enabled) {
      if ((bundle.liveRecords ?? 0) > 0) verbs.push("purge")
      else verbs.push("uninstall")
    }
  } else if ((bundle.liveRecords ?? 0) > 0) {
    verbs.push("purge")
  }

  return (
    <div className="flex shrink-0 flex-col items-end gap-1.5 pt-0.5">
      <div className="flex items-center gap-2">
        {verbs.map((verb) => {
          const plan = verbPlan(verb, bundle)
          return (
            <Button
              key={verb}
              variant={plan.destructive ? "outline" : verb === "enable" ? "default" : "outline"}
              size="sm"
              disabled={mutation.isPending}
              className={plan.destructive ? "text-destructive" : undefined}
              onClick={() => setConfirming(plan)}
            >
              {plan.label}
            </Button>
          )
        })}
      </div>
      {!bundle.installed && (
        <p className="max-w-xs text-right text-xs text-muted-foreground">
          Uninstalled — re-apply the closure to reinstall (
          <span className="data">substratectl apply -f bundle.yaml</span>). Enable
          cannot restore a removed registration.
        </p>
      )}
      {confirming && (
        <Dialog open onOpenChange={(open) => !open && !mutation.isPending && setConfirming(null)}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{confirming.title}</DialogTitle>
              <DialogDescription>{confirming.body}</DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={mutation.isPending}
                onClick={() => setConfirming(null)}
              >
                Cancel
              </Button>
              <Button
                variant={confirming.destructive ? "destructive" : "default"}
                disabled={mutation.isPending}
                onClick={() => mutation.mutate(confirming)}
              >
                {mutation.isPending && <Spinner className="size-3.5" />}
                {confirming.label}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

// ── setup: the declared inputs and what still stands ────────────────────────

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Fragment>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 data">{children}</dd>
    </Fragment>
  )
}

/** One setup item that is NOT an input's own resolution problem: an
 * incomplete OAuth client record ("oauth-client") or an agent's llmprovider
 * row absent or keyless ("provider"). A warning row in the server's own words;
 * a "provider" item links to the named llmprovider record. */
function SetupItemRow({ item, types }: { item: SetupItem; types: KindInfo[] }) {
  // "provider" always means core's llmprovider kind; any other coded item
  // resolves its named kind through the registry for the record route.
  const kind =
    item.code === "provider"
      ? kindByIdentity(types, `${CORE_AUTHORITY}/llmprovider`)
      : item.kind
        ? kindByIdentity(types, item.kind)
        : undefined
  const authority = item.code === "provider" ? CORE_AUTHORITY : kind?.authority
  const plural = item.code === "provider" ? "llmproviders" : kind?.plural
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border border-warning/40 px-4 py-2.5">
      <p className="flex min-w-0 items-center gap-2 text-xs text-warning">
        <TriangleAlertIcon className="size-3.5 shrink-0" />
        <span className="min-w-0">{item.message}</span>
      </p>
      {item.record && authority && plural && (
        <Link
          to="/data/$authority/$plural/$id"
          params={{ authority, plural, id: item.record }}
          className="inline-flex shrink-0 items-center gap-0.5 text-xs text-muted-foreground underline-offset-4 hover:underline"
        >
          <span className="data">{item.record}</span>
          <ArrowUpRightIcon className="size-3" />
        </Link>
      )}
    </div>
  )
}

/** One declared input: its kind, its current resolution (which record, and
 * whether it was bound, the well-known `default`, or the sole live record) or
 * its problem in the server's words, a picker over the live records of its
 * kind with Use this / Unbind (the bind verb), and the create/edit record form
 * for making the first record, after which the `sole` resolution just works. */
function InputCard({
  bundle,
  input,
  types,
}: {
  bundle: BundleStatus
  input: InputStatus
  types: KindInfo[]
}) {
  const queryClient = useQueryClient()
  const kind = kindByIdentity(types, input.kind)
  const authority = kind?.authority ?? splitKind(input.kind).authority
  const [editing, setEditing] = useState<SubstrateRecord | "new" | null>(null)

  const records = useQuery({
    ...recordsQueryOptions({ authority, plural: kind?.plural ?? "", first: 50 }),
    enabled: Boolean(kind),
  })
  const rows = records.data?.records ?? []
  const resolved = input.record
    ? rows.find((r) => r.id === input.record)
    : undefined
  const problem = bundle.setup?.find(
    (item) => item.input === input.name && isInputSetupCode(item.code)
  )

  const bind = useMutation({
    mutationFn: (record: string) =>
      bindBundleInput(bundle.id, input.name, record),
    onSuccess: (status, record) => {
      toast.add({
        type: "success",
        title: record
          ? `${input.name} bound.`
          : `${input.name} unbound.`,
      })
      // The bind verb answers with the refreshed status, the freshest there
      // is. Seed it, then re-read the surfaces that render it.
      seedBundleStatus(queryClient, status)
      refetchBundleStateSoon(queryClient)
    },
    onError: (error, record) => {
      toast.add({
        type: "error",
        title: record
          ? `Could not bind ${input.name}`
          : `Could not unbind ${input.name}`,
        description: error.message,
      })
    },
  })

  return (
    <div className="rounded-md border">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b px-4 py-2.5">
        <div className="min-w-0">
          <span className="font-medium">{input.name}</span>
          <div
            className="truncate data text-xs text-muted-foreground"
            title={input.kind}
          >
            {input.kind}
          </div>
        </div>
        <div className="min-w-0 text-right text-xs">
          {input.record ? (
            <span className="inline-flex items-center gap-1.5 text-muted-foreground">
              record <span className="data">{input.record}</span> via{" "}
              <span className="data">{input.via}</span>
              {kind && (
                <Link
                  to="/data/$authority/$plural/$id"
                  params={{ authority, plural: kind.plural, id: input.record }}
                  className="inline-flex items-center gap-0.5 underline-offset-4 hover:underline"
                >
                  record
                  <ArrowUpRightIcon className="size-3" />
                </Link>
              )}
            </span>
          ) : (
            <span className="inline-flex items-center gap-1.5 text-warning">
              <TriangleAlertIcon className="size-3.5 shrink-0" />
              {problem?.message ?? "Unresolved."}
            </span>
          )}
        </div>
      </div>
      {input.description && (
        <p className="border-b px-4 py-2 text-xs text-muted-foreground">
          {input.description}
        </p>
      )}
      {!kind ? (
        <p className="px-4 py-3 text-xs text-muted-foreground">
          The registry has not reconciled{" "}
          <span className="data">{input.kind}</span>, so its records cannot be
          listed here.
        </p>
      ) : records.isPending ? (
        <Skeleton className="m-3 h-16 rounded-md" />
      ) : records.isError ? (
        <p className="flex items-center gap-2 px-4 py-3 text-xs text-muted-foreground">
          The records didn't load: {records.error.message}
          <Button
            variant="outline"
            size="sm"
            className="h-6 px-2 text-xs"
            onClick={() => void records.refetch()}
          >
            Retry
          </Button>
        </p>
      ) : rows.length === 0 ? (
        <p className="px-4 py-3 text-xs text-muted-foreground">
          No live <span className="data">{splitKind(input.kind).name}</span>{" "}
          record exists yet. Create one and, as the sole record of its kind, it
          resolves this input on its own.
        </p>
      ) : (
        <div>
          {rows.map((record) => {
            const isBoundHere =
              input.via === "bound" && input.record === record.id
            const isResolvedHere = input.record === record.id
            return (
              <div
                key={record.id}
                className="flex items-center justify-between gap-3 border-b px-4 py-2 last:border-0"
              >
                <div className="min-w-0">
                  <span className="truncate text-sm">
                    {recordTitle(record.properties) || record.id}
                  </span>
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="data">{record.id}</span>
                    {isResolvedHere && (
                      <span className="data">in use via {input.via}</span>
                    )}
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 gap-1 px-2 text-xs"
                    onClick={() => setEditing(record)}
                  >
                    <PencilIcon className="size-3" />
                    Edit
                  </Button>
                  {isBoundHere ? (
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-7 px-2 text-xs"
                      disabled={bind.isPending}
                      onClick={() => bind.mutate("")}
                    >
                      Unbind
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-7 px-2 text-xs"
                      disabled={bind.isPending}
                      onClick={() => bind.mutate(record.id)}
                    >
                      Use this
                    </Button>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}
      {resolved && <PropertyGrid record={resolved} />}
      {kind && (
        <div className="flex justify-end border-t px-4 py-2">
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setEditing("new")}
          >
            <PlusIcon className="size-3" />
            Create {kind.name}
          </Button>
        </div>
      )}
      {kind && editing && (
        <RecordConfigForm
          type={kind}
          record={editing === "new" ? undefined : editing}
          open={Boolean(editing)}
          onOpenChange={(open) => !open && setEditing(null)}
          title={
            editing === "new"
              ? `Create ${kind.name}`
              : `Edit ${recordTitle(editing.properties) || kind.name}`
          }
          description={
            <>
              A <span className="data">secret</span> field is write-only. Leave
              it blank on edit to keep the sealed value.
            </>
          }
        />
      )}
    </div>
  )
}

/** The Setup surface: the provider callback URL (integrations only), every
 * setup item that stands on its own, then one card per declared input. The
 * caller renders this ONLY when the bundle declares inputs or the status
 * carries setup items; a bundle needing neither shows nothing at all. */
function SetupSection({
  bundle,
  types,
  provider,
}: {
  bundle: BundleStatus
  types: KindInfo[]
  provider: boolean
}) {
  const standalone = (bundle.setup ?? []).filter(
    (item) => !isInputSetupCode(item.code)
  )
  return (
    <div className="flex flex-col gap-3">
      {provider && <CallbackUrlNote />}
      {standalone.map((item, i) => (
        <SetupItemRow
          key={`${item.code}:${item.input ?? item.record ?? i}`}
          item={item}
          types={types}
        />
      ))}
      {(bundle.inputs ?? []).map((input) => (
        <InputCard
          key={input.name}
          bundle={bundle}
          input={input}
          types={types}
        />
      ))}
    </div>
  )
}

/** A read of a record's properties, grid-aligned (rule 11). Secrets never
 * come over the wire; a blank value reads as unset. */
function PropertyGrid({ record }: { record: SubstrateRecord }) {
  const rows = Object.entries(record.properties).filter(
    ([key]) => key !== "title"
  )
  if (!rows.length) {
    return (
      <p className="px-4 py-3 text-xs text-muted-foreground">
        This record declares no properties beyond its title.
      </p>
    )
  }
  return (
    <dl className="grid grid-cols-[10rem_minmax(0,1fr)] gap-x-4 gap-y-1 px-4 py-3 text-xs">
      {rows.map(([key, value]) => (
        <Fact key={key} label={key}>
          <span className="block truncate" title={cellValue(value)}>
            {cellValue(value) || <span className="text-muted-foreground">—</span>}
          </span>
        </Fact>
      ))}
    </dl>
  )
}

// ── account configs (the trait query) ───────────────────────────────────────

function AccountRow({
  account,
  types,
  disabled,
}: {
  account: SubstrateRecord
  types: KindInfo[]
  disabled: boolean
}) {
  const queryClient = useQueryClient()
  const type = kindByIdentity(types, account.kind)
  const authority = splitKind(account.kind).authority
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)
  // Set once the consent tab is open and awaiting the callback's return
  // message; it gates the postMessage listener below so only the row the owner
  // clicked Connect on reacts.
  const [awaitingReturn, setAwaitingReturn] = useState(false)
  const status =
    typeof account.properties.tokenStatus === "string"
      ? account.properties.tokenStatus
      : ""
  const connected = status === "connected"

  const connect = useMutation({
    mutationFn: async () => {
      // Open the consent tab SYNCHRONOUSLY from the confirm click, before the
      // async start() lands — a tab opened after the round-trip is what popup
      // blockers kill. `about:blank` gives us a handle to navigate. The opener
      // is deliberately KEPT (not severed): the substrate callback returns to
      // the console by calling `window.opener.postMessage({source:"substrate-oauth"
      // …})`, which the listener below awaits — severing opener would strand
      // that return and force the whole-page redirect fallback every time.
      const tab = window.open("about:blank", "_blank")
      try {
        const { url } = await startOAuth(account.id)
        let target: URL
        try {
          target = new URL(url)
        } catch {
          throw new Error("The connect flow returned an invalid authorization URL.")
        }
        if (target.protocol !== "https:") {
          throw new Error(
            "The connect flow returned a non-HTTPS authorization URL; refusing to open it."
          )
        }
        if (tab) tab.location.href = url
        return { opened: Boolean(tab) }
      } catch (error) {
        tab?.close()
        throw error
      }
    },
    onSuccess: ({ opened }) => {
      setConfirming(false)
      if (opened) {
        // The callback posts back on success and we flip the row live then; no
        // manual refresh needed. Arm the listener.
        setAwaitingReturn(true)
        toast.add({
          type: "success",
          title: "Consent opened in a new tab",
          description: "Approve there — this page updates when you return.",
        })
      } else {
        // No tab opened → never claim success. The blocker ate it.
        toast.add({
          type: "error",
          title: "Your browser blocked the consent tab",
          description: "Allow pop-ups for this site, then Connect again.",
        })
      }
      void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
    },
    onError: (error) => {
      setConfirming(false)
      toast.add({
        type: "error",
        title: "Could not start the connect flow",
        description: error.message,
      })
    },
  })

  // The return-to-origin half: while a consent is pending, listen for the
  // substrate callback's postMessage. It ignores everything that is not ours
  // (source !== "substrate-oauth", foreign windows, malformed payloads); on our
  // success it invalidates the account read so the row flips to "connected"
  // (the server is the source of truth — a spoofed message cannot fake it),
  // and on failure it surfaces the correlation id to quote. The listener is
  // torn down as soon as the return lands or the row unmounts.
  useEffect(() => {
    if (!awaitingReturn) return
    function onMessage(event: MessageEvent) {
      const msg = parseSubstrateOAuthMessage(event.data)
      if (!msg) return
      if (msg.ok) {
        // A success names its record; ignore a return meant for another row.
        if (msg.record && msg.record !== account.id) return
        setAwaitingReturn(false)
        setConfirming(false)
        toast.add({
          type: "success",
          title: "Account connected",
          description: recordTitle(account.properties) || account.id,
        })
        void queryClient.invalidateQueries({ queryKey: ["trait", "records"] })
      } else {
        setAwaitingReturn(false)
        toast.add({
          type: "error",
          title: "Connecting failed",
          description: msg.correlation
            ? `The host rejected the grant. Reference ${msg.correlation}.`
            : "The host rejected the grant.",
        })
      }
    }
    window.addEventListener("message", onMessage)
    return () => window.removeEventListener("message", onMessage)
  }, [awaitingReturn, account.id, account.properties, queryClient])

  return (
    <div className="flex items-center justify-between gap-3 border-b px-4 py-2.5 last:border-0">
      <div className="min-w-0">
        <div className="truncate font-medium">
          {recordTitle(account.properties) || account.id}
        </div>
        <div className="flex items-center gap-2 text-xs">
          <span
            className={connected ? "data text-muted-foreground" : "data text-destructive"}
          >
            {status || "not connected"}
          </span>
          {type && (
            <Link
              to="/data/$authority/$plural/$id"
              params={{ authority: authority, plural: type.plural, id: account.id }}
              className="inline-flex items-center gap-0.5 text-muted-foreground underline-offset-4 hover:underline"
            >
              record
              <ArrowUpRightIcon className="size-3" />
            </Link>
          )}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {type && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setEditing(true)}
          >
            <PencilIcon className="size-3" />
            Edit
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={disabled || connect.isPending}
          onClick={() => setConfirming(true)}
        >
          {connect.isPending && <Spinner className="size-3.5" />}
          {connected ? "Reconnect" : "Connect account"}
        </Button>
      </div>
      {confirming && (
        <Dialog
          open
          onOpenChange={(open) => !open && !connect.isPending && setConfirming(false)}
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>
                {connected ? "Reconnect" : "Connect"}{" "}
                {recordTitle(account.properties) || "this account"}?
              </DialogTitle>
              <DialogDescription>
                This opens the provider's consent screen in a new tab. On
                approval the host stores a credential reference on this account
                and begins syncing the enabled data.
                {connected &&
                  " Reconnecting replaces the current grant — the previous consent is superseded."}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={connect.isPending}
                onClick={() => setConfirming(false)}
              >
                Cancel
              </Button>
              <Button
                disabled={connect.isPending}
                onClick={() => connect.mutate()}
              >
                {connect.isPending && <Spinner className="size-3.5" />}
                {connected ? "Reconnect" : "Connect"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
      {type && editing && (
        <RecordConfigForm
          type={type}
          record={account}
          open={editing}
          onOpenChange={setEditing}
          title={`Edit ${recordTitle(account.properties) || type.name}`}
          description="Change which data this account syncs, the cadence and the backfill depth. Token state is host-managed and not editable here."
        />
      )}
    </div>
  )
}

function AccountsSection({ bundle, types }: { bundle: BundleStatus; types: KindInfo[] }) {
  const accounts = useQuery(traitRecordsQueryOptions(ACCOUNT_CONFIG_TRAIT))
  const [adding, setAdding] = useState(false)
  const accountType = useMemo(
    () => accountKindOf(types, bundle.authority),
    [types, bundle.authority]
  )
  // The trait query spans every bundle; scope to this one by the owned
  // authority.
  const mine = useMemo(
    () =>
      (accounts.data?.records ?? []).filter(
        (e) => splitKind(e.kind).authority === bundle.authority
      ),
    [accounts.data, bundle.authority]
  )
  const capped = accounts.data?.capped ?? false
  // oauth/start refuses while the client input is unresolved or its record is
  // incomplete; the gate here is UX, stated before the server states it. Only
  // the CLIENT input's setup steps block connecting.
  const blocked =
    !bundle.installed || !bundle.enabled || oauthConnectBlocked(bundle, types)

  const addButton = accountType ? (
    <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
      <PlusIcon className="size-3.5" />
      Add account
    </Button>
  ) : null

  const addDialog =
    accountType && adding ? (
      <RecordConfigForm
        type={accountType}
        open={adding}
        onOpenChange={setAdding}
        title={`Add ${accountType.name}`}
        description="Create the account, then Connect it to run the host OAuth flow. The feature toggles and cadence take effect once it is connected."
      />
    ) : null

  if (accounts.isPending) {
    return <Skeleton className="h-20 w-full rounded-md" />
  }
  if (accounts.isError) {
    return (
      <p className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
        The accounts didn't load — {accounts.error.message}
        <Button
          variant="outline"
          size="sm"
          className="h-6 px-2 text-xs"
          onClick={() => void accounts.refetch()}
        >
          Retry
        </Button>
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {mine.length > 0 && (
        <div className="flex items-center justify-end">{addButton}</div>
      )}
      {!mine.length ? (
        accountType ? (
          <Empty className="rounded-md border py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <PlugZapIcon />
              </EmptyMedia>
              <EmptyTitle>No accounts yet</EmptyTitle>
              <EmptyDescription>
                Add an account, then connect it to run the host OAuth flow.
              </EmptyDescription>
            </EmptyHeader>
            <div className="flex justify-center pt-1">
              <Button onClick={() => setAdding(true)}>
                <PlusIcon className="size-3.5" />
                Add account
              </Button>
            </div>
          </Empty>
        ) : (
          <p className="rounded-md border px-4 py-3 text-xs text-muted-foreground">
            This integration declares no account-config kind.
          </p>
        )
      ) : (
        <div className="rounded-md border">
          {blocked && (
            <p className="border-b bg-muted/40 px-4 py-2 text-xs text-muted-foreground">
              Connecting is refused until the integration is installed, enabled
              and its OAuth client is set up.
            </p>
          )}
          {capped && (
            <p className="border-b bg-muted/40 px-4 py-2 text-xs text-muted-foreground">
              Showing the first accounts only — this substrate has more than the
              embedded list caps at. Open the account type to browse them all.
            </p>
          )}
          {mine.map((account) => (
            <AccountRow
              key={account.id}
              account={account}
              types={types}
              disabled={blocked}
            />
          ))}
        </div>
      )}
      {addDialog}
    </div>
  )
}

// ── closure inventory: the Kinds + Records tables ───────────────────────────

/** The live row count for one installed kind, walked on demand (the API serves
 * no count — a bounded keyset walk over the list cursor, capped as N+). A kind
 * the registry has not resolved to a collection has no count to walk. */
function KindRowCount({ row }: { row: KindRow }) {
  const resolved = Boolean(row.authority && row.plural)
  const count = useQuery({
    ...recordCountQueryOptions(row.authority ?? "", row.plural ?? ""),
    enabled: resolved,
  })
  if (!resolved || count.isError) {
    return <span className="text-muted-foreground">—</span>
  }
  if (count.isPending) {
    return <Skeleton className="ml-auto h-3.5 w-10" />
  }
  return <span className="data tabular-nums">{formatCount(count.data)}</span>
}

function kindColumns(): ColumnDef<KindRow, unknown>[] {
  return [
    {
      id: "kind",
      accessorFn: (t) => t.name,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="kind" />,
      cell: ({ row }) => {
        const t = row.original
        const name = (
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <span className="truncate font-medium">{t.name}</span>
            {t.role && (
              <Badge variant="outline" className="shrink-0 text-[10px] font-normal">
                {t.role}
              </Badge>
            )}
          </span>
        )
        return (
          <div className="min-w-0">
            {t.authority && t.plural ? (
              <Link
                to="/data/$authority/$plural"
                params={{ authority: t.authority, plural: t.plural }}
                className="underline-offset-4 hover:underline"
              >
                {name}
              </Link>
            ) : (
              name
            )}
            <div
              className="truncate data text-xs text-muted-foreground"
              title={t.identity}
            >
              {t.identity}
            </div>
          </div>
        )
      },
      meta: { label: "kind", size: { min: 220, weight: 2 } },
    },
    {
      id: "rows",
      accessorFn: () => "",
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="rows" align="right" />
      ),
      cell: ({ row }) => <KindRowCount row={row.original} />,
      meta: {
        label: "rows",
        width: 96,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
  ]
}

/** Where a shipped record opens. An agent has its chat surface; every other
 * record opens in Data at its own kind, which needs that kind's plural from the
 * registry — a record of a kind this repository has not installed has nowhere to
 * go, and renders as text. Functions and mappings have no detail route yet. */
function ShippedRecordName({
  row,
  types,
}: {
  row: ShippedRecordRow
  types: KindInfo[]
}) {
  const name = (
    <div className="min-w-0">
      <div className="truncate font-medium">{row.name}</div>
      <div className="truncate data text-xs text-muted-foreground" title={row.id}>
        {row.id}
      </div>
    </div>
  )
  if (row.kind === `${CORE_AUTHORITY}/agent`) {
    return (
      <Link
        to="/agents/$id"
        params={{ id: row.id }}
        className="block min-w-0 underline-offset-4 hover:underline"
      >
        {name}
      </Link>
    )
  }
  const k = kindByIdentity(types, row.kind)
  if (!k?.authority || !k.plural || NO_DETAIL_ROUTE.has(row.kind)) {
    return name
  }
  return (
    <Link
      to="/data/$authority/$plural/$id"
      params={{ authority: k.authority, plural: k.plural, id: row.id }}
      className="block min-w-0 underline-offset-4 hover:underline"
    >
      {name}
    </Link>
  )
}

/** Declarations the console has no record page for: they are records, but the
 * Data surface does not serve the core meta-kinds that hold them. */
const NO_DETAIL_ROUTE = new Set([
  `${CORE_AUTHORITY}/function`,
  `${CORE_AUTHORITY}/recordmapping`,
])

/** One icon per shipped-record kind; anything a bundle ships that is not one of
 * these is an ordinary record and gets the record icon. */
const RECORD_ICON: Record<string, typeof BotIcon> = {
  [`${CORE_AUTHORITY}/function`]: FunctionSquareIcon,
  [`${CORE_AUTHORITY}/agent`]: BotIcon,
  [`${CORE_AUTHORITY}/trigger`]: ZapIcon,
  [`${CORE_AUTHORITY}/recordmapping`]: BoxesIcon,
}

/** Kind FIRST, then the record: the kind is what the reader scans this table
 * by, and it is the same order the Data surface reads in. */
function shippedRecordColumns(types: KindInfo[]): ColumnDef<ShippedRecordRow, unknown>[] {
  return [
    {
      id: "kind",
      accessorFn: (r) => r.kind,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="kind" />,
      cell: ({ row }) => {
        const Icon = RECORD_ICON[row.original.kind] ?? BoxIcon
        return (
          <span
            className="inline-flex min-w-0 items-center gap-1.5 text-muted-foreground"
            title={row.original.kind}
          >
            <Icon className="size-3.5 shrink-0" />
            <span className="truncate data">{splitKind(row.original.kind).name}</span>
          </span>
        )
      },
      meta: { label: "kind", width: 160 },
    },
    {
      id: "record",
      accessorFn: (r) => r.name,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="record" />
      ),
      cell: ({ row }) => <ShippedRecordName row={row.original} types={types} />,
      meta: { label: "record", size: { min: 220, weight: 2 } },
    },
  ]
}

/** The bundle's installed record kinds with their live row counts. Rows come
 * from the closure (catalog) or, for a bundle applied without a catalog entry,
 * the registry's own view of the owned authority. */
function KindsSection({
  bundle,
  types,
  catalog,
}: {
  bundle: BundleStatus
  types: KindInfo[]
  catalog?: CatalogItem
}) {
  const rows = useMemo(
    () => installedKindRows(bundle, types, catalog),
    [bundle, types, catalog]
  )
  const columns = useMemo(() => kindColumns(), [])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (r) => r.identity,
  })
  return (
    <DataTable
      table={table}
      density="compact"
      empty={
        <Empty className="py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BoxesIcon />
            </EmptyMedia>
            <EmptyTitle>No record kinds</EmptyTitle>
            <EmptyDescription>
              This bundle installed no record kinds.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      }
    />
  )
}

/** The records the bundle ships beside its kinds — its functions, agents and
 * mappings, and the data rows the install wrote (triggers, provider rows). Read
 * from the shipped catalog; a bundle with no catalog entry shows the empty
 * state rather than a guess. */
function RecordsSection({
  catalog,
  types,
}: {
  catalog?: CatalogItem
  types: KindInfo[]
}) {
  const rows = useMemo(() => bundleRecordRows(catalog), [catalog])
  const columns = useMemo(() => shippedRecordColumns(types), [types])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (r) => `${r.kind}:${r.id}`,
  })
  return (
    <DataTable
      table={table}
      density="compact"
      empty={
        <Empty className="py-10">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FunctionSquareIcon />
            </EmptyMedia>
            <EmptyTitle>No other records</EmptyTitle>
            <EmptyDescription>
              This bundle ships no functions, agents, mappings or data records.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      }
    />
  )
}

// ── the page ────────────────────────────────────────────────────────────────

export function BundleDetailPage() {
  const { id } = bundleDetailRoute.useParams()
  const status = useQuery(bundleStatusQueryOptions(id))
  const registry = useQuery(kindsQueryOptions)
  const catalog = useQuery(catalogItemQueryOptions(id))
  const types = registry.data ?? []

  if (status.isPending) return <DetailSkeleton />

  if (status.isError) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The bundle didn't load</EmptyTitle>
            <EmptyDescription>
              <span className="data">{id}</span> — {status.error.message}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" size="sm" onClick={() => void status.refetch()}>
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  const bundle = status.data
  const provider = declaresProviderInterfaces(bundle, types)
  const item = catalog.data
  const vocabulary = item?.vocabulary ?? false
  // The Setup surface exists only for a bundle that declares inputs or whose
  // status carries setup items. A vocabulary bundle (the loader forbids it
  // inputs) shows nothing there: no heading, no empty state.
  const hasSetup =
    (bundle.inputs?.length ?? 0) > 0 || (bundle.setup?.length ?? 0) > 0
  // The requirements are checked against the LIVE registry: an authority is
  // present when some reconciled kind carries it, which is the check the
  // server's admission makes.
  const requirements = requirementsOf(
    { requires: item?.requires ?? [] },
    presentAuthorities([], types)
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <h1 className="text-lg font-semibold">{bundle.name}</h1>
            <BundleStateBadge state={bundleState(bundle)} />
            <SetupBadge count={setupCount(bundle)} />
            {bundle.quarantined && bundle.quarantineReason ? (
              <span className="basis-full text-xs text-warning">
                Quarantined: {bundle.quarantineReason} Re-install the bundle
                to clear it.
              </span>
            ) : null}
            {vocabulary ? (
              <Badge variant="outline" className="shrink-0 font-normal">
                Vocabulary
              </Badge>
            ) : item?.integration ? (
              <Badge variant="outline" className="shrink-0 font-normal">
                Integration
              </Badge>
            ) : null}
          </div>
          <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <Link
              to="/data/$authority/$plural/$id"
              params={{ authority: "core.substrate.reamde.dev", plural: "bundles", id: bundle.id }}
              className="inline-flex items-center gap-0.5 underline-offset-4 hover:underline"
            >
              <span className="data">{bundle.authority}</span>
              <ArrowUpRightIcon className="size-3" />
            </Link>
            {item?.version && <span className="data">· {item.version}</span>}
          </p>
          {item?.description && (
            <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
              {item.description}
            </p>
          )}
        </div>
        <LifecycleButtons bundle={bundle} />
      </div>

      <div className="min-h-0 flex-1 overflow-auto px-6 pb-6">
        <div className="flex flex-col gap-6">
          {requirements.length > 0 && (
            <RequiresNote requirements={requirements} />
          )}
          {hasSetup && (
            <section>
              <h2 className="pb-1 text-sm font-medium">Setup</h2>
              <p className="pb-2 text-xs text-muted-foreground">
                {(bundle.inputs?.length ?? 0) > 0 ? (
                  <>
                    Each declared input resolves ONE record: an explicitly
                    bound one first, then the record named{" "}
                    <span className="data">default</span>, then the sole live
                    record of its kind.
                  </>
                ) : (
                  <>
                    What this bundle still needs before the paths it ships will
                    run, in the server's own words.
                  </>
                )}
              </p>
              {registry.isPending ? (
                <Skeleton className="h-24 w-full rounded-md" />
              ) : (
                <SetupSection
                  bundle={bundle}
                  types={types}
                  provider={provider}
                />
              )}
            </section>
          )}
          {provider && (
            <section>
              <h2 className="pb-1 text-sm font-medium">Accounts</h2>
              <p className="pb-2 text-xs text-muted-foreground">
                The integration's connected accounts (a{" "}
                <span className="data">accountconfig</span> trait query); the
                host runs the OAuth flow.
              </p>
              <AccountsSection bundle={bundle} types={types} />
            </section>
          )}
          <section>
            <h2 className="pb-1 text-sm font-medium">Kinds</h2>
            <p className="pb-2 text-xs text-muted-foreground">
              The record kinds this bundle installed in{" "}
              <span className="data">{bundle.authority}</span>, each with its live
              row count.
            </p>
            {registry.isPending ? (
              <Skeleton className="h-24 w-full rounded-md" />
            ) : (
              <div className="rounded-md border">
                <KindsSection
                  bundle={bundle}
                  types={types}
                  catalog={catalog.data}
                />
              </div>
            )}
          </section>
          <section>
            <h2 className="pb-1 text-sm font-medium">Records</h2>
            <p className="pb-2 text-xs text-muted-foreground">
              The rest of the closure — its functions, agents and mappings, and
              the records the install wrote beside them.
            </p>
            {catalog.isPending || registry.isPending ? (
              <Skeleton className="h-24 w-full rounded-md" />
            ) : (
              <div className="rounded-md border">
                <RecordsSection catalog={catalog.data} types={types} />
              </div>
            )}
          </section>
        </div>
      </div>
    </div>
  )
}

function DetailSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-3">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="mt-1.5 h-3.5 w-72" />
      </div>
      <div className="flex flex-col gap-3 px-6">
        <Skeleton className="h-32 w-full rounded-md" />
        <Skeleton className="h-24 w-full rounded-md" />
      </div>
    </div>
  )
}
