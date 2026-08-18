/** Dashboard zone: the data section at a glance — authorities → kinds with
 * live counts, every row a door into that kind's browse and every authority
 * title a door into the authority page. Counts are probe walks (see
 * api/overview.ts) so the zone is deliberately cheap two ways: rows paint
 * from the registry at once and the probes wait until every OTHER zone has
 * its data (a one-way latch on the query cache going idle — the glance
 * answers "is everything okay" before the ledger starts counting), and the
 * walks run behind the shared concurrency gate and cache for minutes.
 *
 * It shows the repository's OWN authorities. The substrate's machinery (core
 * and whatever a bundle installed) is not a glance question — it is always
 * there, and counting its two dozen kinds cost the overview a probe walk each
 * — so it lives in the data nav, one click away, and nowhere on this page. */

import { useState } from "react"
import { useIsFetching, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { ZoneHeader } from "@/components/home/zone"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { formatCount } from "@/lib/api/records"
import { authorityCountsQueryOptions } from "@/lib/api/overview"
import { isMachineryAuthority, type AuthorityNav } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"

function AuthorityCard({ nav, armed }: { nav: AuthorityNav; armed: boolean }) {
  const counts = useQuery({
    ...authorityCountsQueryOptions(nav.authority, nav.kinds),
    enabled: armed,
  })
  const countOf = (k: KindInfo) =>
    counts.data?.find((c) => c.kind.identity === k.identity)?.count

  return (
    <Card size="sm" className="gap-1.5">
      <CardHeader>
        <CardTitle className="data text-xs font-medium text-muted-foreground">
          <Link
            to="/data/$authority"
            params={{ authority: nav.authority }}
            className="underline-offset-4 hover:underline"
          >
            {nav.authority}
          </Link>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {/* rule 11: a real grid with a reserved count column — the numbers
            align down the card, no loose whitespace */}
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-4 gap-y-1">
          {nav.kinds.map((k) => {
            const count = countOf(k)
            return (
              <span key={k.identity} className="contents">
                <Link
                  to="/data/$authority/$name"
                  params={{ authority: k.authority, name: k.name }}
                  className="truncate data text-xs underline-offset-4 hover:underline"
                >
                  {k.name}
                </Link>
                {counts.data !== undefined ? (
                  count === undefined ? (
                    // The substrate refused this one collection (or the walk
                    // failed) — the row stays a door, the number stays honest.
                    <span className="text-xs text-muted-foreground">—</span>
                  ) : (
                    <span className="text-right data text-xs text-muted-foreground">
                      {formatCount(count)}
                    </span>
                  )
                ) : counts.isError ? (
                  <span className="text-xs text-muted-foreground">—</span>
                ) : (
                  <Skeleton className="h-3.5 w-8 justify-self-end" />
                )}
              </span>
            )
          })}
        </div>
      </CardContent>
    </Card>
  )
}

// items-start: each authority card is its own height — a two-kind authority
// must not stretch to the media authority's dozen rows (codex finding,
// 2026-08-06).
const AUTHORITY_GRID = "grid items-start gap-3 sm:grid-cols-2 xl:grid-cols-3"

export function DataCountsZone({
  authorities,
}: {
  authorities: AuthorityNav[]
}) {
  const own = authorities.filter(
    (a) => !isMachineryAuthority(a.authority, a.kinds)
  )
  // The latch: probes hold until the rest of the dashboard has fetched —
  // the count zone's own (disabled) queries never keep it closed, and once
  // open it stays open across the other zones' periodic refetches.
  const busyElsewhere = useIsFetching({
    predicate: (q) =>
      !(q.queryKey[0] === "overview" && q.queryKey[1] === "counts"),
  })
  const [armed, setArmed] = useState(false)
  // The guarded render-time latch (react.dev "adjusting state when props
  // change") — it flips exactly once, the first time the cache goes idle.
  if (!armed && busyElsewhere === 0) setArmed(true)

  return (
    <section className="flex flex-col gap-2.5">
      <ZoneHeader
        title="Data"
        to={
          own[0]
            ? `/data/${own[0].authority}`
            : authorities[0]
              ? `/data/${authorities[0].authority}`
              : "/"
        }
        linkLabel="Browse"
      />
      {own.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          No schema authorities are declared yet — install a vocabulary bundle
          from the registry, or declare a kind of your own.
        </p>
      ) : (
        <div className={AUTHORITY_GRID}>
          {own.map((nav) => (
            <AuthorityCard key={nav.authority} nav={nav} armed={armed} />
          ))}
        </div>
      )}
    </section>
  )
}
