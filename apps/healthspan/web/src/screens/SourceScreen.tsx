import { ActionRow, ProblemBox, Row, Section, Spinner } from "@substrate/react"
import { describe } from "@substrate/client"
import { useMemo, useState } from "react"
import { useBackend } from "../data"
import { formatDate } from "../lib/format"
import { go, routeHash } from "../route"
import { METRIC_GROUPS, SESSION_GROUPS } from "../sources/groups"
import { classWord, metricIndex, metricName } from "../sources/list"
import { linksOf } from "../sources/links"
import { metricOrder } from "../sources/ranks"
import { EmptyState } from "../ui/EmptyState"
import { Screen } from "../ui/Screen"
import { ImportItem } from "../ui/sources/ImportItem"
import { LinkSheet } from "../ui/sources/LinkSheet"
import { useSessionOrders } from "./SourcesScreen"

/** One source: what it reports and when last, its rank in each group it
 * is in, the sources it is linked with, and the imports from it. */
export function SourceScreen(props: { id: string }) {
  const sources = useBackend().sources
  const read = sources.useSourceList()
  const metricsRead = sources.useMetrics()
  const links = sources.useSourceLinks().data ?? []
  const imports = sources.useImports().data ?? []
  const writes = sources.useSourceWrites()
  const [linking, setLinking] = useState(false)
  const list = useMemo(() => read.data ?? [], [read.data])
  const metrics = useMemo(
    () => metricIndex(metricsRead.data ?? []),
    [metricsRead.data]
  )
  const sessions = useSessionOrders(list, links)
  const s = list.find((x) => x.id === props.id)
  const back = {
    label: "Sources",
    onClick: () => go({ screen: "sources" }),
  }

  if (!s) {
    return (
      <Screen title="Source" back={back}>
        {read.loading ? (
          <div className="flex justify-center py-16 text-secondary">
            <Spinner />
          </div>
        ) : read.error ? (
          <Section>
            <ProblemBox
              role="alert"
              title="Couldn't read your sources"
              message={describe(read.error)}
              action={{ title: "Try Again", onClick: read.refresh }}
            />
          </Section>
        ) : (
          <EmptyState
            title="Not in your repository"
            description="This source may have been removed."
            action={{ label: "All sources", onClick: back.onClick }}
          />
        )}
      </Screen>
    )
  }

  const names = new Map(list.map((x) => [x.id, x.name]))
  const ranks = [
    ...METRIC_GROUPS.map((g) => {
      const order = metricOrder(g, list)
      return {
        label: g.label,
        at: order.findIndex((x) => x.id === s.id),
        of: order.length,
      }
    }),
    ...SESSION_GROUPS.map((g) => ({
      label: g.label,
      at: sessions[g.id].findIndex((x) => x.id === s.id),
      of: sessions[g.id].length,
    })),
  ].filter((r) => r.at >= 0)
  const mine = linksOf(s.id, links)
  const fromHere = imports.filter((i) => i.source === s.id)
  const metricsShown = [...s.metrics].sort((a, b) =>
    metricName(a.metric, metrics).localeCompare(metricName(b.metric, metrics))
  )

  return (
    <Screen title={s.name} subtitle={classWord(s.class)} back={back}>
      <Section header="What it reports">
        {metricsShown.map((m) => (
          <Row
            key={m.metric}
            label={metricName(m.metric, metrics)}
            value={m.latest ? formatDate(m.latest) : "No value yet"}
          />
        ))}
        {s.nights > 0 && (
          <Row label="Nights" value={s.nights.toLocaleString("en-GB")} />
        )}
        {s.workouts > 0 && (
          <Row label="Workouts" value={s.workouts.toLocaleString("en-GB")} />
        )}
        {s.reports > 0 && (
          <Row label="Lab reports" value={s.reports.toLocaleString("en-GB")} />
        )}
        {!metricsShown.length && !s.nights && !s.workouts && !s.reports && (
          <Row label="Nothing read yet" />
        )}
      </Section>

      {ranks.length > 0 && (
        <Section
          header="Its ranks"
          footer={
            <>
              Measured values follow your repository&rsquo;s ranks, nights and
              workouts this app&rsquo;s. Change them on{" "}
              <a
                href={routeHash({ screen: "sources" })}
                className="text-accent-text"
              >
                Sources
              </a>
              .
            </>
          }
        >
          {ranks.map((r) => (
            <Row
              key={r.label}
              label={r.label}
              value={r.of > 1 ? `${r.at + 1} of ${r.of}` : "The only source"}
            />
          ))}
        </Section>
      )}

      <Section
        header="One device"
        footer="Link it with the other sources that are the same device, so its values count once."
      >
        {mine.map((l) => (
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
            <button
              type="button"
              onClick={() => void writes.unlink(l)}
              className="min-h-11 text-[15px] text-danger"
            >
              Unlink
            </button>
          </div>
        ))}
        <ActionRow onClick={() => setLinking(true)}>
          Link With Another Source…
        </ActionRow>
      </Section>

      {fromHere.length > 0 && (
        <Section header="Imports from it">
          {fromHere.map((imp) => (
            <ImportItem key={imp.id} imp={imp} />
          ))}
        </Section>
      )}

      <LinkSheet
        open={linking}
        onClose={() => setLinking(false)}
        start={s.id}
        sources={[...list].sort((a, b) => a.name.localeCompare(b.name))}
        orders={{
          nights: sessions.nights.map((x) => x.id),
          workouts: sessions.workouts.map((x) => x.id),
        }}
      />
    </Screen>
  )
}
