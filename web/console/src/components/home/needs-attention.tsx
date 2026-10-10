/** Home's count of open alerts (decision record 0148), each linking to the
 * page where it lives: the agent's or the tool's page, which lists the alert
 * with its detail. Nothing renders while nothing is open, so a healthy Home
 * is unchanged. It is a count and a set of links, not an inbox (decision
 * 0130): nothing is decided here. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import {
  AlertLevelPill,
  AlertRecordLink,
} from "@/components/alerts/alerts-panel"
import { SectionHead } from "@/components/identity/section-head"
import { RowList } from "@/components/providers/provider-marks"
import { alertPlace, readAlert, type Alert, ALERT_KIND } from "@/lib/alerts"
import { openAlertsQueryOptions } from "@/lib/api/alerts"
import { splitKind } from "@/lib/api/http"
import { relativeTime } from "@/lib/format"

/** The alerts Home names before "See all alerts" takes over. */
const SHOWN_ALERTS = 5

const LINK =
  "min-w-0 flex-1 font-medium break-words text-foreground underline-offset-2 hover:underline"

function PlaceLink({ alert }: { alert: Alert }) {
  const place = alertPlace(alert)
  if (place?.to === "agent") {
    return (
      <Link to="/agents/$id" params={{ id: place.id }} className={LINK}>
        {alert.summary}
      </Link>
    )
  }
  if (place?.to === "tool") {
    return (
      <Link
        to="/tools/$authority/$pkg/$name"
        params={{
          authority: place.authority,
          pkg: place.pkg,
          name: place.name,
        }}
        className={LINK}
      >
        {alert.summary}
      </Link>
    )
  }
  return (
    <AlertRecordLink id={alert.id} className={LINK}>
      {alert.summary}
    </AlertRecordLink>
  )
}

export function NeedsAttention() {
  const alerts = useQuery(openAlertsQueryOptions)
  if (alerts.isPending) return null
  if (alerts.isError) {
    return (
      <p className="mt-6 text-muted-foreground">
        Open alerts didn’t load: {alerts.error.message}{" "}
        <button
          type="button"
          className="cursor-pointer underline"
          onClick={() => void alerts.refetch()}
        >
          Try again
        </button>
      </p>
    )
  }
  const { count } = alerts.data
  const open = alerts.data.records.map(readAlert).filter((a) => a.open)
  if (count === 0 || open.length === 0) return null
  const shown = open.slice(0, SHOWN_ALERTS)
  const { authority, pkg, name } = splitKind(ALERT_KIND)
  return (
    <section aria-labelledby="home-needs-attention">
      <SectionHead
        id="home-needs-attention"
        title="Needs attention"
        hint={`${count} open ${count === 1 ? "alert" : "alerts"}`}
      />
      <RowList>
        {shown.map((a) => (
          <div
            key={a.id}
            data-slot="attention-row"
            className="flex items-start gap-2 px-3 py-2.5 text-[13px]"
          >
            <AlertLevelPill level={a.level} />
            <PlaceLink alert={a} />
            <span className="shrink-0 text-[12.5px] whitespace-nowrap text-faint">
              <span>Last seen </span>
              <span title={a.lastSeenAt}>{relativeTime(a.lastSeenAt)}</span>
            </span>
          </div>
        ))}
      </RowList>
      {count > shown.length && (
        <p className="mt-2 text-[12.5px] text-muted-foreground">
          And {count - shown.length} more.{" "}
          <Link
            to="/data/$authority/$pkg/$name"
            params={{ authority, pkg, name }}
          >
            See all alerts
          </Link>
        </p>
      )}
    </section>
  )
}
