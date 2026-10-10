/** The open alerts about one place's records (decision record 0148): a
 * provider's, an agent's or a tool's page passes the record paths it stands
 * for, and the panel lists the open alerts whose `about` names any of them.
 * Nothing renders while nothing is wrong, so a healthy page is unchanged. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { Pill, type PillTone } from "@/components/identity/pill"
import { SectionHead } from "@/components/identity/section-head"
import { RowList } from "@/components/providers/provider-marks"
import {
  ALERT_KIND,
  detailLine,
  readAlert,
  type Alert,
  type AlertLevel,
} from "@/lib/alerts"
import { openAlertsAboutQueryOptions } from "@/lib/api/alerts"
import { splitKind } from "@/lib/api/http"
import { relativeTime } from "@/lib/format"

const LEVEL: Record<AlertLevel, { label: string; tone: PillTone }> = {
  error: { label: "Error", tone: "bad" },
  warning: { label: "Warning", tone: "warn" },
  info: { label: "Info", tone: "neutral" },
}

export function AlertLevelPill({ level }: { level: AlertLevel }) {
  return <Pill tone={LEVEL[level].tone}>{LEVEL[level].label}</Pill>
}

/** What an alert's count counts, named from its key's source. */
function countLabel(alert: Alert): string {
  return alert.key.startsWith("trigger.parked/") ? "Parked deliveries" : "Count"
}

/** A link to an alert's own record: its full detail, and where the owner
 * resolves it by hand. */
export function AlertRecordLink({
  id,
  children,
  className,
}: {
  id: string
  children: React.ReactNode
  className?: string
}) {
  const { authority, pkg, name } = splitKind(ALERT_KIND)
  return (
    <Link
      to="/data/$authority/$pkg/$name/$id"
      params={{ authority, pkg, name, id }}
      className={className}
    >
      {children}
    </Link>
  )
}

function AlertRow({ alert }: { alert: Alert }) {
  const detail = detailLine(alert)
  return (
    <div data-slot="alert-row" className="px-3 py-2.5 text-[13px]">
      <div className="flex items-start gap-2">
        <AlertLevelPill level={alert.level} />
        <span className="min-w-0 flex-1 font-medium break-words">
          {alert.summary}
        </span>
      </div>
      {detail && (
        <p className="mt-1 break-words text-muted-foreground">
          <span className="text-faint">Last error: </span>
          {detail}
        </p>
      )}
      <dl className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 text-[12.5px]">
        <div className="flex gap-1">
          <dt className="text-faint">First seen</dt>
          <dd title={alert.firstSeenAt}>{relativeTime(alert.firstSeenAt)}</dd>
        </div>
        <div className="flex gap-1">
          <dt className="text-faint">Last seen</dt>
          <dd title={alert.lastSeenAt}>{relativeTime(alert.lastSeenAt)}</dd>
        </div>
        {alert.count !== undefined && (
          <div className="flex gap-1">
            <dt className="text-faint">{countLabel(alert)}</dt>
            <dd className="tabular-nums">{alert.count}</dd>
          </div>
        )}
        <div>
          <AlertRecordLink
            id={alert.id}
            className="font-medium text-primary-text underline-offset-2 hover:underline"
          >
            Full details
          </AlertRecordLink>
        </div>
      </dl>
    </div>
  )
}

export function AlertsPanel({
  refs,
  className,
}: {
  /** The record paths this page stands for: the alerts whose `about` names
   * any of them are listed. */
  refs: readonly string[]
  className?: string
}) {
  const alerts = useQuery(openAlertsAboutQueryOptions(refs))
  if (alerts.isError) {
    return (
      <p className="mt-6 text-[13px] text-muted-foreground">
        Its alerts didn’t load: {alerts.error.message}{" "}
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
  const open = (alerts.data ?? []).map(readAlert).filter((a) => a.open)
  if (!open.length) return null
  return (
    <section aria-labelledby="needs-attention" className={className}>
      <SectionHead
        id="needs-attention"
        title="Needs attention"
        hint={`${open.length} open ${open.length === 1 ? "alert" : "alerts"}`}
      />
      <RowList>
        {open.map((a) => (
          <AlertRow key={a.id} alert={a} />
        ))}
      </RowList>
    </section>
  )
}
