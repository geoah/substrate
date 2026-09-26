/** What a provider adds, in two lists. WHAT IT BRINGS IN: its primary kinds,
 * each with what it is, how many records it holds, and which of your own
 * kinds its records fill in (off the record mappings); the supporting kinds
 * are counted, not listed, and technical mode lists every kind with its
 * purpose. A kind a provider's functions write says when it last synced. ITS TOOLS: the functions it ships, each with when it runs and how
 * its last run went, linking to the tool's own page. */

import { useState } from "react"
import { useQueries, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { IdText } from "@/components/identity/id-text"
import { KindRef } from "@/components/identity/kind-ref"
import { SectionHead } from "@/components/identity/section-head"
import { Pill } from "@/components/identity/pill"
import { RowList, ToneText } from "@/components/providers/provider-marks"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { recordMappingsListQueryOptions } from "@/lib/api/bundles"
import {
  functionsQueryOptions,
  recentTriggerRunsQueryOptions,
} from "@/lib/api/functions"
import { formatCount, recordCountQueryOptions } from "@/lib/api/records"
import {
  triggerRecordsQueryOptions,
  triggerStatusesQueryOptions,
} from "@/lib/api/sync"
import type { CatalogItem, KindInfo } from "@/lib/api/types"
import { kindPackage, kindPurpose, splitKind } from "@/lib/definition"
import { relativeTime } from "@/lib/format"
import {
  fillsIn,
  lastSyncOf,
  providerTools,
  toolActivity,
} from "@/lib/providers"
import { kindDescription } from "@/lib/kind-copy"
import { cn } from "@/lib/utils"

const PURPOSE_WORD = {
  primary: "Collection",
  supporting: "Supporting",
  internal: "Internal",
} as const

interface KindLine {
  reference: string
  kind?: KindInfo
  description?: string
  purpose: keyof typeof PURPOSE_WORD
}

function kindLines(
  bundleId: string,
  kinds: KindInfo[],
  catalog: CatalogItem | undefined
): KindLine[] {
  const held = kinds.filter((k) => kindPackage(k) === bundleId)
  if (held.length) {
    return held
      .map((k) => ({
        reference: k.identity,
        kind: k,
        description: k.description || undefined,
        purpose: kindPurpose(k),
      }))
      .sort((a, b) => a.reference.localeCompare(b.reference))
  }
  // Not here yet: the catalog's closure is the only word on what it adds.
  const described = catalog?.closure.kindDescriptions ?? {}
  return (catalog?.closure.kinds ?? []).map((reference) => ({
    reference,
    description: described[reference],
    purpose: kindPurpose(reference),
  }))
}

function lineText(line: KindLine, technical: boolean): string | undefined {
  return kindDescription(
    line.kind ?? line.reference,
    technical,
    line.description
  )
}

/** Past this many characters a description likely runs over two lines. The
 * whole paragraph belongs on the kind's Definition tab, so the list shows two
 * and a disclosure. */
const LONG_DESCRIPTION = 140

function TwoLines({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  const long = text.length > LONG_DESCRIPTION
  return (
    <>
      <p
        className={cn(
          "mt-0.5 text-muted-foreground",
          long && !open && "line-clamp-2"
        )}
      >
        {text}
      </p>
      {long && (
        <button
          type="button"
          aria-expanded={open}
          className="cursor-pointer text-[12.5px] text-faint underline-offset-2 hover:text-foreground hover:underline"
          onClick={() => setOpen((v) => !v)}
        >
          {open ? "less" : "more"}
        </button>
      )}
    </>
  )
}

export function BringsIn({
  bundleId,
  kinds,
  catalog,
  installed,
  title = "What it brings in",
}: {
  bundleId: string
  kinds: KindInfo[]
  catalog: CatalogItem | undefined
  installed: boolean
  title?: string
}) {
  const [technical] = useTechnicalDetails()
  const mappings = useQuery({
    ...recordMappingsListQueryOptions,
    enabled: installed,
  })
  // When each collection last synced: off the runs of the functions that
  // write it. A sample's kinds have none, and an empty collection says
  // "None yet" alone: a run that brought nothing in is no news.
  const functions = useQuery({ ...functionsQueryOptions, enabled: installed })
  const triggers = useQuery({
    ...triggerRecordsQueryOptions,
    enabled: installed,
  })
  const runs = useQuery({
    ...recentTriggerRunsQueryOptions,
    enabled: installed,
  })
  const lines = kindLines(bundleId, kinds, catalog)
  const primary = lines.filter((l) => l.purpose === "primary")
  const rest = lines.length - primary.length
  const shown = technical ? lines : primary
  const counts = useQueries({
    queries: shown.map((l) => {
      const { authority, pkg, name } = splitKind(l.reference)
      return {
        ...recordCountQueryOptions(authority, pkg, name),
        enabled: installed,
      }
    }),
  })
  if (!lines.length) return null
  return (
    <section aria-labelledby="brings-in">
      <SectionHead
        id="brings-in"
        title={title}
        hint={`${primary.length} ${primary.length === 1 ? "collection" : "collections"} you’ll see${
          rest > 0
            ? `; ${rest} more ${rest === 1 ? "holds" : "hold"} supporting details`
            : ""
        }`}
      />
      <RowList>
        {shown.map((line, i) => {
          const count = counts[i]
          const targets = fillsIn(mappings.data ?? [], line.reference)
          const synced = installed
            ? lastSyncOf(
                line.reference,
                bundleId,
                functions.data ?? [],
                triggers.data ?? [],
                runs.data ?? []
              )
            : undefined
          return (
            <div
              key={line.reference}
              data-slot="brings-in-row"
              className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 px-3 py-2.5 text-[13px]"
            >
              <div className="min-w-0">
                <KindRef kind={line.kind ?? line.reference} link={installed} />
                {lineText(line, technical) &&
                  (technical ? (
                    <TwoLines text={lineText(line, technical)!} />
                  ) : (
                    <p
                      className="mt-0.5 line-clamp-1 text-muted-foreground"
                      title={line.description}
                    >
                      {lineText(line, technical)}
                    </p>
                  ))}
                {targets.map((t) => (
                  <p
                    key={t}
                    className="mt-0.5 flex flex-wrap items-center gap-1.5 text-muted-foreground"
                  >
                    <span>Also fills in your</span>
                    <KindRef kind={t} />
                  </p>
                ))}
              </div>
              <div className="flex flex-col items-end gap-0.5 text-muted-foreground tabular-nums">
                <div className="flex items-center gap-2">
                  {technical && (
                    <Pill tone="neutral" dot={false}>
                      {PURPOSE_WORD[line.purpose]}
                    </Pill>
                  )}
                  {installed &&
                    (count?.isPending ? (
                      <Skeleton className="h-3.5 w-10" />
                    ) : count?.data ? (
                      count.data.value === 0 ? (
                        <span className="text-faint">None yet</span>
                      ) : (
                        `${formatCount(count.data)} ${count.data.value === 1 ? "record" : "records"}`
                      )
                    ) : null)}
                </div>
                {synced && (count?.data?.value ?? 0) > 0 && (
                  <span
                    data-slot="last-sync"
                    className="text-[12px] text-faint"
                    title={synced}
                  >
                    Synced {relativeTime(synced)}
                  </span>
                )}
              </div>
            </div>
          )
        })}
      </RowList>
    </section>
  )
}

export function ProviderTools({
  catalog,
  installed,
}: {
  catalog: CatalogItem | undefined
  installed: boolean
}) {
  const [technical] = useTechnicalDetails()
  const triggers = useQuery({
    ...triggerRecordsQueryOptions,
    enabled: installed,
  })
  const statuses = useQuery({
    ...triggerStatusesQueryOptions,
    enabled: installed,
  })
  const tools = providerTools(
    catalog?.closure.functions ?? [],
    catalog?.closure.functionDescriptions,
    triggers.data ?? []
  )
  if (!tools.length) return null
  return (
    <section aria-labelledby="its-tools">
      <SectionHead
        id="its-tools"
        title="Its tools"
        hint="What it runs to keep your copy up to date"
      />
      <RowList>
        {tools.map((tool) => {
          const { authority, pkg, name } = splitKind(tool.reference)
          const activity = toolActivity(tool.triggerIds, statuses.data ?? [])
          return (
            <div
              key={tool.reference}
              data-slot="tool-row"
              className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 px-3 py-2.5 text-[13px]"
            >
              <div className="min-w-0">
                <Link
                  to="/tools/$authority/$pkg/$name"
                  params={{ authority, pkg, name }}
                  className="font-medium underline-offset-2 hover:underline"
                >
                  {tool.name}
                </Link>
                <p className="mt-0.5 text-muted-foreground">
                  {tool.cadences.length
                    ? tool.cadences.join(" · ")
                    : "When it is called"}
                </p>
                {technical && <IdText value={tool.reference} />}
              </div>
              <div className="text-right text-[12.5px] whitespace-nowrap">
                {!installed ? null : activity.parked > 0 ? (
                  <ToneText tone="bad">
                    {activity.parked} {activity.parked === 1 ? "run" : "runs"}{" "}
                    failed
                  </ToneText>
                ) : activity.lastFire ? (
                  <ToneText tone="muted">
                    Ran {relativeTime(activity.lastFire)}
                  </ToneText>
                ) : (
                  <ToneText tone="muted">It hasn’t run yet</ToneText>
                )}
              </div>
            </div>
          )
        })}
      </RowList>
    </section>
  )
}
