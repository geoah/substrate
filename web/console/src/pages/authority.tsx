/** Authority page (`/data/:authority`) and package page
 * (`/data/:authority/:package`): the collections one authority publishes,
 * grouped by package, or the collections of one package — each a row with
 * its glyph and plural, what it holds and how many records it has, and a door
 * into its collection. Everyday mode lists the primary collections and says
 * how many supporting ones it leaves out; technical mode lists every kind
 * with its full reference and its purpose. Bounded registry data, so no
 * paging. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { FileCode2Icon } from "lucide-react"

import { CopyButton } from "@/components/identity/copy-button"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath } from "@/components/identity/kind-ref"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { SectionHead } from "@/components/identity/section-head"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { PROVIDERS_AUTHORITY, providerInfo } from "@/lib/actor-identity"
import { authorityTitle, packageTitle } from "@/lib/collections"
import { formatCount, recordCountQueryOptions } from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import { kindPurpose } from "@/lib/definition"
import { hiddenKindsNote } from "@/lib/grid-values"
import { displayPlural } from "@/lib/kind-names"
import { authorityRoute, packageRoute } from "@/router"
import { kindDescription } from "@/lib/kind-copy"

function CountCell({ kind }: { kind: KindInfo }) {
  const count = useQuery(
    recordCountQueryOptions(kind.authority, kind.package, kind.name)
  )
  if (count.isPending) return <Skeleton className="ml-auto h-3.5 w-8" />
  if (count.isError) return <span className="text-faint">—</span>
  return <span className="tabular-nums">{formatCount(count.data)}</span>
}

function KindsList({ kinds }: { kinds: KindInfo[] }) {
  const [technical] = useTechnicalDetails()
  const navigate = useNavigate()
  const shown = technical
    ? kinds
    : kinds.filter((k) => kindPurpose(k) === "primary")
  const hidden = kinds.filter((k) => !shown.includes(k))
  return (
    <>
      {shown.length > 0 && (
        <div className="overflow-x-auto rounded-[10px] border border-border">
          <table className="w-full min-w-[560px] table-fixed border-separate border-spacing-0 text-sm">
            <colgroup>
              <col className="w-[240px]" />
              {technical && <col className="w-[340px]" />}
              <col />
              <col className="w-[90px]" />
            </colgroup>
            <thead>
              <tr className="[&>th]:h-[34px] [&>th]:border-b [&>th]:border-border [&>th]:px-2.5 [&>th]:text-left [&>th]:text-[12.5px] [&>th]:font-medium [&>th]:text-faint [&>th+th]:border-l">
                <th scope="col">Collection</th>
                {technical && <th scope="col">Reference</th>}
                <th scope="col">What it holds</th>
                <th scope="col" className="text-right!">
                  Records
                </th>
              </tr>
            </thead>
            <tbody>
              {shown.map((k) => {
                const params = {
                  authority: k.authority,
                  pkg: k.package,
                  name: k.name,
                }
                const purpose = kindPurpose(k)
                return (
                  <tr
                    key={k.identity}
                    className="cursor-pointer [&:hover>td]:bg-[color-mix(in_oklab,var(--background)_96%,var(--foreground))] [&:last-child>td]:border-b-0 [&>td]:h-[38px] [&>td]:overflow-hidden [&>td]:border-b [&>td]:border-border [&>td]:px-2.5 [&>td]:whitespace-nowrap [&>td+td]:border-l"
                    onClick={() =>
                      void navigate({
                        to: "/data/$authority/$pkg/$name",
                        params,
                      })
                    }
                  >
                    <td className="font-medium">
                      <span className="flex min-w-0 items-center gap-2">
                        <KindGlyph kind={k} size="sm" />
                        <Link
                          to="/data/$authority/$pkg/$name"
                          params={params}
                          className="truncate underline-offset-[3px] hover:underline hover:decoration-border-strong"
                          onClick={(e) => e.stopPropagation()}
                        >
                          {displayPlural(k)}
                        </Link>
                        {technical && purpose !== "primary" && (
                          <span className="shrink-0 rounded-[3px] border border-border-strong px-1 text-[10.5px] font-normal text-faint">
                            {purpose}
                          </span>
                        )}
                      </span>
                    </td>
                    {technical && (
                      <td>
                        <span className="block truncate">
                          <KindPath reference={k.identity} />
                        </span>
                      </td>
                    )}
                    <td className="text-muted-foreground">
                      <span
                        className="block truncate"
                        title={k.description || undefined}
                      >
                        {kindDescription(k, technical) || (
                          <span className="text-faint">—</span>
                        )}
                      </span>
                    </td>
                    <td className="text-right">
                      <CountCell kind={k} />
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {hidden.length > 0 && (
        <p className="mx-0.5 mt-2 max-w-[80ch] text-[12.5px] text-faint">
          {hiddenKindsNote(hidden)}
        </p>
      )}
    </>
  )
}

function PageState({
  pending,
  error,
  empty,
}: {
  pending: boolean
  error?: Error | null
  empty: boolean
}) {
  if (pending) {
    return (
      <div className="mt-6 flex flex-col gap-2">
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    )
  }
  if (!error && !empty) return null
  return (
    <Empty className="mt-6">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FileCode2Icon />
        </EmptyMedia>
        <EmptyTitle>
          {error ? "These collections didn't load" : "Nothing here"}
        </EmptyTitle>
        <EmptyDescription>
          {error
            ? `${error.message}. Reload the page to try again.`
            : "Nothing in your substrate is published here."}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

function countWord(n: number): string {
  return `${n} ${n === 1 ? "collection" : "collections"}`
}

/** Every collection one authority publishes, a section per package. */
export function AuthorityPage() {
  const { authority } = authorityRoute.useParams()
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(
    () =>
      (registry.data ?? [])
        .filter((k) => k.authority === authority)
        .sort(
          (a, b) =>
            a.package.localeCompare(b.package) ||
            displayPlural(a).localeCompare(displayPlural(b))
        ),
    [registry.data, authority]
  )
  const packages = useMemo(() => {
    const out = new Map<string, KindInfo[]>()
    for (const k of kinds) {
      out.set(k.package, [...(out.get(k.package) ?? []), k])
    }
    return [...out.entries()]
  }, [kinds])
  const primary = kinds.filter((k) => kindPurpose(k) === "primary").length
  const providers = authority === PROVIDERS_AUTHORITY
  return (
    <TablePage>
      <PageHeader
        title={
          technical ? (
            <span className="font-mono">{authority}</span>
          ) : (
            authorityTitle(authority)
          )
        }
        meta={
          <>
            <span>
              {countWord(technical ? kinds.length : primary)}
              {packages.length > 1 && ` in ${packages.length} packages`}
            </span>
            {technical && (
              <CopyButton value={authority} label="Copy the authority" />
            )}
          </>
        }
        description={
          providers
            ? "Collections providers publish. Their records are copies, kept up to date by each provider."
            : "The collections published under this name."
        }
      />
      <PageState
        pending={registry.isPending}
        error={registry.isError ? registry.error : null}
        empty={!registry.isPending && !kinds.length}
      />
      {packages.map(([pkg, list]) => (
        <section key={pkg}>
          <SectionHead
            title={
              <>
                {providers && <ProviderBadge provider={pkg} size="sm" />}
                <Link
                  to="/data/$authority/$pkg"
                  params={{ authority, pkg }}
                  className="underline-offset-[3px] hover:underline"
                >
                  {technical && !providers ? pkg : packageTitle(authority, pkg)}
                </Link>
              </>
            }
            hint={
              technical && (
                <span className="font-mono text-[12px]">
                  {authority}/{pkg}
                </span>
              )
            }
          />
          <KindsList kinds={list} />
        </section>
      ))}
    </TablePage>
  )
}

/** The collections of ONE package, the group a declaration is versioned and
 * quarantined in (decision 0047). */
export function PackagePage() {
  const { authority, pkg } = packageRoute.useParams()
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  const kinds = useMemo(
    () =>
      (registry.data ?? [])
        .filter((k) => k.authority === authority && k.package === pkg)
        .sort((a, b) => displayPlural(a).localeCompare(displayPlural(b))),
    [registry.data, authority, pkg]
  )
  const primary = kinds.filter((k) => kindPurpose(k) === "primary").length
  const provider = authority === PROVIDERS_AUTHORITY ? providerInfo(pkg) : null
  return (
    <TablePage>
      <PageHeader
        title={
          <span className="flex items-center gap-2.5">
            {provider && <ProviderBadge provider={provider} size="md" />}
            {technical && !provider ? (
              <span className="font-mono">{pkg}</span>
            ) : (
              packageTitle(authority, pkg)
            )}
          </span>
        }
        meta={
          <>
            {technical && (
              <span className="inline-flex items-center gap-1">
                <span className="font-mono text-[12.5px]">
                  <span className="text-faint">{authority}/</span>
                  <span className="text-foreground">{pkg}</span>
                </span>
                <CopyButton
                  value={`${authority}/${pkg}`}
                  label="Copy the package"
                />
              </span>
            )}
            <span>{countWord(technical ? kinds.length : primary)}</span>
          </>
        }
        description={
          provider
            ? `Read-only copies, kept up to date by ${provider.name}.`
            : undefined
        }
      />
      <PageState
        pending={registry.isPending}
        error={registry.isError ? registry.error : null}
        empty={!registry.isPending && !kinds.length}
      />
      {kinds.length > 0 && (
        <div className="mt-6">
          <KindsList kinds={kinds} />
        </div>
      )}
    </TablePage>
  )
}
