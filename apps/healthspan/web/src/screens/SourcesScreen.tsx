import { ActionRow, BarButton, ProblemBox, Section } from "@substrate/react"
import { describe } from "@substrate/client"
import { useMemo, useState } from "react"
import type { MetricRecord } from "../catalogue/types"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import type { SourceSummary, SourceWrites } from "../data/sources"
import { go } from "../route"
import {
  METRIC_GROUPS,
  SESSION_GROUPS,
  inGroup,
  type SessionKind,
} from "../sources/groups"
import { byClass, metricIndex, metricName } from "../sources/list"
import { keepLinkedTogether, linksOf, type SourceLink } from "../sources/links"
import { metricOrder, sessionOrder } from "../sources/ranks"
import { EmptyState } from "../ui/EmptyState"
import { TextButton } from "../ui/IconButton"
import { Screen } from "../ui/Screen"
import { SkeletonRow } from "../ui/Skeleton"
import { EnterResultSheet } from "../ui/import/EnterResultSheet"
import { ImportItem } from "../ui/sources/ImportItem"
import { LinkSheet } from "../ui/sources/LinkSheet"
import { RankList, type RankItem } from "../ui/sources/RankList"
import { SourceRow } from "../ui/sources/SourceRow"

/** The orders shown for nights and workouts, which a link moves together
 * and the rank lists write. */
export function useSessionOrders(
  list: SourceSummary[],
  links: SourceLink[]
): Record<SessionKind, SourceSummary[]> {
  const profile = useBackend().profile.useOwner().data?.profile
  return useMemo(
    () => ({
      nights: sessionOrder("nights", profile?.nightSources ?? [], list, links),
      workouts: sessionOrder(
        "workouts",
        profile?.workoutSources ?? [],
        list,
        links
      ),
    }),
    [profile?.nightSources, profile?.workoutSources, list, links]
  )
}

/** Every source by class, its ranks per group, the sources linked as one
 * device, and the imports (docs/plans/healthspan.md, Sources). */
export function SourcesScreen() {
  const { today } = useMode()
  const sources = useBackend().sources
  const read = sources.useSourceList()
  const metricsRead = sources.useMetrics()
  const links = sources.useSourceLinks().data ?? []
  const imports = sources.useImports()
  const writes = sources.useSourceWrites()
  const [linking, setLinking] = useState(false)
  const [entering, setEntering] = useState(false)
  const list = useMemo(() => read.data ?? [], [read.data])
  const metrics = useMemo(
    () => metricIndex(metricsRead.data ?? []),
    [metricsRead.data]
  )
  const sessions = useSessionOrders(list, links)
  const names = new Map(list.map((s) => [s.id, s.name]))

  return (
    <Screen
      title="Sources"
      actions={
        <BarButton onClick={() => go({ screen: "import" })}>Import</BarButton>
      }
    >
      {read.loading && !read.data ? (
        <Section>
          {[0, 1, 2].map((i) => (
            <SkeletonRow key={i} lines={3} hasAvatar={false} />
          ))}
        </Section>
      ) : read.error && !read.data ? (
        <Section>
          <ProblemBox
            role="alert"
            title="Couldn't read your sources"
            message={describe(read.error)}
            action={{ title: "Try Again", onClick: read.refresh }}
          />
        </Section>
      ) : !list.length ? (
        <EmptyState
          title="No sources yet"
          description="Connect a provider in your console, or import a file of lab results."
          action={{
            label: "Import a file",
            onClick: () => go({ screen: "import" }),
          }}
        />
      ) : (
        byClass(list).map((g) => (
          <Section key={g.header} header={g.header}>
            {g.sources.map((s) => (
              <SourceRow
                key={s.id}
                source={s}
                metrics={metrics}
                today={today}
              />
            ))}
          </Section>
        ))
      )}

      {list.length > 0 && (
        <Ranks
          list={list}
          metrics={metrics}
          links={links}
          sessions={sessions}
          writes={writes}
        />
      )}

      {list.length > 1 && (
        <Section
          header="One device"
          footer="Sources that are one device, such as a band whose nights also arrive through a phone. A metric read takes their values once, and the night and workout orders keep them together."
        >
          {links.map((l) => (
            <div
              key={l.id}
              className="flex min-h-11 flex-wrap items-center gap-x-3 px-4 py-2.5"
            >
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="text-[17px] break-words">{l.name}</span>
                <span className="text-[15px] break-words text-secondary">
                  {l.sources.map((id) => names.get(id) ?? id).join(", ")}
                </span>
              </span>
              <TextButton
                className="text-danger"
                onClick={() => void writes.unlink(l)}
              >
                Unlink
              </TextButton>
            </div>
          ))}
          <ActionRow onClick={() => setLinking(true)}>
            Link Sources as One Device…
          </ActionRow>
        </Section>
      )}

      <Section header="Imports">
        {(imports.data ?? []).map((imp) => (
          <ImportItem
            key={imp.id}
            imp={imp}
            sourceName={imp.source ? names.get(imp.source) : undefined}
          />
        ))}
        <ActionRow onClick={() => go({ screen: "import" })}>
          Import a File…
        </ActionRow>
        <ActionRow onClick={() => setEntering(true)}>Enter a Result…</ActionRow>
      </Section>

      <LinkSheet
        open={linking}
        onClose={() => setLinking(false)}
        sources={[...list].sort((a, b) => a.name.localeCompare(b.name))}
        orders={{
          nights: sessions.nights.map((s) => s.id),
          workouts: sessions.workouts.map((s) => s.id),
        }}
      />
      <EnterResultSheet open={entering} onClose={() => setEntering(false)} />
    </Screen>
  )
}

/** A session rank's line: how many it holds, and the device it is one of. */
function sessionDetail(
  kind: SessionKind,
  s: SourceSummary,
  list: SourceSummary[],
  links: SourceLink[]
): string {
  const n = kind === "nights" ? s.nights : s.workouts
  const held = `${n.toLocaleString("en-GB")} ${n === 1 ? kind.slice(0, -1) : kind}`
  const others = linksOf(s.id, links)
    .flatMap((l) => l.sources)
    .filter((id, i, all) => id !== s.id && all.indexOf(id) === i)
    .map((id) => list.find((x) => x.id === id)?.name ?? id)
  return others.length
    ? `${held}, one device with ${others.join(" and ")}`
    : held
}

function Ranks(props: {
  list: SourceSummary[]
  metrics: ReadonlyMap<string, MetricRecord>
  links: SourceLink[]
  sessions: Record<SessionKind, SourceSummary[]>
  writes: SourceWrites
}) {
  const { list, metrics, sessions, writes } = props
  const metricGroups = METRIC_GROUPS.map((g) => ({
    group: g,
    order: metricOrder(g, list),
  })).filter((x) => x.order.length > 1)
  const sessionGroups = SESSION_GROUPS.map((g) => ({
    ...g,
    order: sessions[g.id],
  })).filter((x) => x.order.length > 1)
  if (!metricGroups.length && !sessionGroups.length) return null

  const metricItems = (
    g: (typeof METRIC_GROUPS)[number],
    order: SourceSummary[]
  ): RankItem[] =>
    order.map((s) => {
      const names = s.metrics
        .filter((m) => inGroup(g, m.metric))
        .map((m) => metricName(m.metric, metrics))
      return {
        id: s.id,
        name: s.name,
        detail:
          names.length > 3
            ? `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`
            : names.join(", "),
      }
    })

  return (
    <>
      {metricGroups.length > 0 && (
        <Section
          header="Ranks of measured values"
          footer="These ranks are your repository's: every app that reads these metrics takes the first source's value where several overlap. Drag a source by its handle, or use Move up and Move down."
        >
          {metricGroups.map(({ group, order }) => (
            <RankList
              key={group.id}
              title={group.label}
              items={metricItems(group, order)}
              onReorder={(ids) =>
                writes.rankMetrics(
                  group,
                  ids.flatMap((id) => order.filter((s) => s.id === id))
                )
              }
            />
          ))}
        </Section>
      )}
      {sessionGroups.length > 0 && (
        <Section
          header="Ranks of nights and workouts"
          footer="These ranks are this app's own: they decide which source's night or workout it reads where several recorded the same one."
        >
          {sessionGroups.map((g) => (
            <RankList
              key={g.id}
              title={g.label}
              items={g.order.map((s) => ({
                id: s.id,
                name: s.name,
                detail: sessionDetail(g.id, s, list, props.links),
              }))}
              onReorder={(ids) =>
                writes.rankSessions(g.id, keepLinkedTogether(ids, props.links))
              }
            />
          ))}
        </Section>
      )}
    </>
  )
}
