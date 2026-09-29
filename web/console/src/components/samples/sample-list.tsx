/** The shipped samples one "Add …" entry offers, each by what it adds, with
 * **Add** (the providers' own door: the packages it needs land first) or
 * "Added". Updating a copy is its package page's job, not this list's. */

import { useMemo, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { CheckIcon } from "lucide-react"

import { IdText } from "@/components/identity/id-text"
import { TakeButton } from "@/components/providers/bundle-actions"
import { Skeleton } from "@/components/ui/skeleton"
import { joinWords } from "@/lib/agent-chat"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { catalogQueryOptions } from "@/lib/api/catalog"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import type { KindInfo } from "@/lib/api/types"
import {
  heldVersions,
  mergeBundles,
  missingChain,
  presentPackages,
  requirementTree,
  type BundleRow,
  type RequirementNode,
} from "@/lib/bundles"
import { packageDisplayName } from "@/lib/kind-names"
import type { SamplePick } from "./sample-picks"

export function SampleList({
  pick,
  member,
  empty,
}: {
  pick: (rows: BundleRow[], registry: KindInfo[]) => SamplePick[]
  /** One member of a sample, as its row shows it. */
  member: (id: string, registry: readonly KindInfo[]) => ReactNode
  /** What the list says when no sample qualifies. */
  empty: string
}) {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const catalog = useQuery(catalogQueryOptions)
  const repository = useQuery(repositoryQueryOptions)
  const registry = useQuery(kindsQueryOptions)
  const home = repository.data?.authority ?? getRepository() ?? ""

  const rows = useMemo(
    () => mergeBundles(statuses.data ?? [], catalog.data ?? [], home),
    [statuses.data, catalog.data, home]
  )
  const chains = useMemo(() => {
    const present = presentPackages(rows, registry.data ?? [])
    const versions = heldVersions(rows)
    return new Map(
      rows.map((row) => [
        row.key,
        requirementTree(row, rows, present, versions),
      ])
    )
  }, [rows, registry.data])
  const samples = useMemo(
    () => pick(rows, registry.data ?? []),
    [pick, rows, registry.data]
  )
  // Two publishers' samples of one package word read alike; the shipped id
  // is what tells them, and the Add, apart.
  const shared = useMemo(() => {
    const seen = new Set<string>()
    const twice = new Set<string>()
    for (const { row } of samples) {
      const name = packageDisplayName(row.name)
      if (seen.has(name)) twice.add(name)
      seen.add(name)
    }
    return twice
  }, [samples])

  if (catalog.isPending || statuses.isPending || registry.isPending) {
    return (
      <div className="flex flex-col gap-2">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
    )
  }
  if (catalog.isError) {
    return (
      <p className="text-muted-foreground">
        The samples didn’t load: {catalog.error.message}
      </p>
    )
  }
  if (!samples.length) {
    return <p className="text-muted-foreground">{empty}</p>
  }
  return (
    <ul className="max-h-80 overflow-y-auto rounded-[10px] border border-border">
      {samples.map((sample) => (
        <SampleRow
          key={sample.row.key}
          sample={sample}
          chain={chains.get(sample.row.key) ?? []}
          showSource={shared.has(packageDisplayName(sample.row.name))}
        >
          {sample.members.map((id) => (
            <span key={id} className="inline-flex items-center gap-1.5">
              {member(id, registry.data ?? [])}
            </span>
          ))}
        </SampleRow>
      ))}
    </ul>
  )
}

function SampleRow({
  sample: { row },
  chain,
  showSource,
  children,
}: {
  sample: SamplePick
  chain: RequirementNode[]
  /** Says which shipped sample this is: another row has the same name. */
  showSource: boolean
  children: ReactNode
}) {
  const missing = missingChain(chain)
  const name = packageDisplayName(row.name)
  return (
    <li className="border-b border-border px-3.5 py-3 last:border-b-0">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="font-medium">{name}</div>
          {showSource && row.catalog && (
            <p className="text-[12.5px] text-faint">
              From <IdText value={row.catalog.id} />
            </p>
          )}
          <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px] text-muted-foreground">
            {children}
          </p>
          {!row.installed && missing.length > 0 && (
            <p className="mt-1 text-[12.5px] text-faint">
              Adds{" "}
              {joinWords(
                missing.map((m) => packageDisplayName(m.row?.name ?? m.package))
              )}{" "}
              first, which it needs.
            </p>
          )}
        </div>
        {row.installed ? (
          <span className="inline-flex shrink-0 items-center gap-1 pt-0.5 text-[12.5px] text-ok">
            <CheckIcon className="size-3.5" />
            Added
          </span>
        ) : (
          <TakeButton
            row={row}
            chain={chain}
            name={name}
            label="Add"
            variant="default"
          />
        )}
      </div>
    </li>
  )
}
