import {
  ActionRow,
  BarButton,
  Field,
  NoteRow,
  ProblemBox,
  Row,
  Section,
  Spinner,
  cx,
} from "@substrate/react"
import { describe, uuidv7 } from "@substrate/client"
import { Check } from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"
import { CATALOGUE } from "../../catalogue"
import { useBackend } from "../../data"
import {
  ROLES,
  detectColumns,
  normalise,
  missingRoles,
  roleLabel,
  type ColumnRole,
} from "../../import/columns"
import type { Table } from "../../import/csv"
import { planRows } from "../../import/importers/csv"
import {
  matchRows,
  pickKey,
  readRows,
  tickedAtFirst,
  type ResolvedRow,
} from "../../import/resolve"
import { detectDayOrder, type DayOrder } from "../../import/values"
import { formatDate } from "../../lib/format"
import { go } from "../../route"
import { metricIndex } from "../../sources/list"
import { Badge } from "../Badge"
import { TextButton } from "../IconButton"
import { SegmentedControl } from "../ToggleGroup"
import { rowRange, rowValue } from "./PlanPreview"

/** A column's row: its header and a sample over the role picker. */
function ColumnRow(props: {
  header: string
  sample?: string
  role: ColumnRole
  onChange: (role: ColumnRole) => void
}) {
  return (
    <label className="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5">
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="text-[17px] break-words">
          {props.header || "Unnamed column"}
        </span>
        {props.sample && (
          <span className="text-[13px] break-words text-secondary">
            Such as {props.sample}
          </span>
        )}
      </span>
      <select
        value={props.role}
        onChange={(e) => props.onChange(e.target.value as ColumnRole)}
        aria-label={`What ${props.header || "this column"} holds`}
        className="h-9 rounded-[8px] bg-fill px-2 text-[15px] text-label outline-none focus-visible:ring-2 focus-visible:ring-accent"
      >
        {ROLES.map((r) => (
          <option key={r.role} value={r.role}>
            {r.label}
          </option>
        ))}
      </select>
    </label>
  )
}

/** A row to tick: its test, the metric it goes to, its value and range,
 * and the "may already be here" mark. */
function TickRow(props: {
  row: ResolvedRow
  ticked: boolean
  onToggle: () => void
}) {
  const { row } = props
  const range = rowRange(row.range)
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={props.ticked}
      onClick={props.onToggle}
      className="flex min-h-11 w-full items-start gap-3 px-4 py-2.5 text-left active:bg-selected"
    >
      <span
        aria-hidden
        className={cx(
          "mt-0.5 flex size-[22px] shrink-0 items-center justify-center rounded-[6px]",
          props.ticked ? "bg-accent text-white" : "ring-[1.5px] ring-secondary"
        )}
      >
        {props.ticked && <Check className="size-4" />}
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[17px] break-words">{row.name ?? row.loinc}</span>
        {row.metric &&
          normalise(row.metric.name) !== normalise(row.name ?? "") && (
            <span className="text-[13px] break-words text-secondary">
              As {row.metric.name}
            </span>
          )}
        {row.held && (
          <span>
            <Badge
              label="May already be here"
              tone="warning"
              title="The repository holds a result of this test on this day with the same value. Tick it to import it anyway."
            />
          </span>
        )}
      </span>
      <span className="flex shrink-0 flex-col items-end">
        <span className="text-[17px] text-secondary tabular-nums">
          {rowValue(row)}
        </span>
        {(range || row.flag) && (
          <span className="text-[13px] text-secondary">
            {[range, row.flag && row.flag !== "N" ? `Flag ${row.flag}` : ""]
              .filter(Boolean)
              .join(", ")}
          </span>
        )}
      </span>
    </button>
  )
}

/** The day order a table's dates read in, or undefined where every date
 * reads both ways. */
function dayOrderOf(table: Table): DayOrder | undefined {
  const dateCol = detectColumns(table.headers).indexOf("date")
  return dateCol < 0 ? "dmy" : detectDayOrder(table.rows.map((r) => r[dateCol]))
}

/** A file of lab results read through the CSV importer: its columns, the
 * day order, the lab, the rows ticked, the plan, and Import: each lab's
 * plan staged as an import, then run. */
export function CsvImport(props: {
  name: string
  table: Table
  onBack: () => void
}) {
  const sources = useBackend().sources
  const actions = sources.useImportActions()
  const metricsRead = sources.useLabMetrics()
  const metrics = useMemo(() => metricsRead.data ?? [], [metricsRead.data])
  const file = { name: props.name, table: props.table }
  const [roles, setRoles] = useState<ColumnRole[]>(() =>
    detectColumns(props.table.headers)
  )
  const [order, setOrder] = useState<DayOrder | undefined>(() =>
    dayOrderOf(props.table)
  )
  const [orderAsked] = useState(() => dayOrderOf(props.table) === undefined)
  const [lab, setLab] = useState("")
  const [picks, setPicks] = useState<Map<string, string>>(new Map())
  const [matched, setMatched] = useState<ResolvedRow[] | null>(null)
  const [matching, setMatching] = useState(false)
  const [matchError, setMatchError] = useState<string>()
  const [toggled, setToggled] = useState<Map<number, boolean>>(new Map())
  const [starting, setStarting] = useState(false)
  const [startError, setStartError] = useState<string>()

  const missing = missingRoles(roles)
  const labColumn = roles.includes("lab")
  const needsLab =
    !labColumn || file.table.rows.some((r) => !r[roles.indexOf("lab")]?.trim())

  // Matching runs again whenever what it reads changes; an older run's
  // answer is dropped.
  const runId = useRef(0)
  useEffect(() => {
    if (missing.length || !order || !metricsRead.data) return
    const id = ++runId.current
    const rows = readRows(file.table, roles, order, lab)
    const t = setTimeout(() => {
      setMatching(true)
      setMatchError(undefined)
      matchRows(rows, {
        catalogue: CATALOGUE,
        metrics,
        picks,
        convert: actions.convert,
        held: actions.held,
      }).then(
        (out) => {
          if (id !== runId.current) return
          setMatched(out)
          setMatching(false)
        },
        (e: unknown) => {
          if (id !== runId.current) return
          setMatchError(describe(e))
          setMatching(false)
        }
      )
    }, 250)
    return () => clearTimeout(t)
    // `missing` follows `roles`, and the file is the screen's for its life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roles, order, lab, picks, metrics, metricsRead.data, actions])

  const isTicked = (r: ResolvedRow) =>
    !r.problem && (toggled.get(r.line) ?? tickedAtFirst(r))
  const ticked = useMemo(
    () => new Set((matched ?? []).filter(isTicked).map((r) => r.line)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [matched, toggled]
  )
  const plan = useMemo(
    () => (matched ? planRows(matched, ticked, props.name) : []),
    [matched, ticked, props.name]
  )
  const planned = plan.reduce((n, p) => n + p.rows.length, 0)
  const ready =
    !!matched && !matching && planned > 0 && !starting && !missing.length

  const start = async () => {
    setStarting(true)
    setStartError(undefined)
    try {
      const requests = plan.map((p) => ({
        id: uuidv7(),
        plan: p.plan,
        lab: p.lab,
      }))
      const outcomes = await actions.start(requests)
      const failed = outcomes.find((o) => o.state !== "planned")
      if (failed) {
        setStartError(
          failed.state === "failed"
            ? failed.words
            : "It could not be sent to your repository. Try again."
        )
        return
      }
      go(
        requests.length === 1
          ? { screen: "import", id: requests[0].id }
          : { screen: "sources" }
      )
    } catch (e) {
      setStartError(describe(e))
    } finally {
      setStarting(false)
    }
  }

  const sample = (i: number) => file.table.rows.find((r) => r[i]?.trim())?.[i]
  const index = metricIndex(metrics)
  const writable = (matched ?? []).filter((r) => !r.problem)
  const leftOut = (matched ?? []).filter((r) => r.problem)
  const groups = new Map<string, ResolvedRow[]>()
  for (const r of writable) {
    const key = `${r.lab ?? ""}\u0000${r.accession ?? r.day ?? ""}`
    const g = groups.get(key) ?? []
    g.push(r)
    groups.set(key, g)
  }
  const labMetrics = [...metrics]
    .filter((m) => m.category === "labs" || m.category === "body")
    .sort((a, b) => a.name.localeCompare(b.name))

  return (
    <>
      <Section>
        <Row label="File" value={file.name} />
        <Row
          label="Rows"
          value={file.table.rows.length.toLocaleString("en-GB")}
        />
        <ActionRow onClick={props.onBack}>Choose Another File…</ActionRow>
      </Section>

      <Section
        header="Columns"
        footer="Each column's role is read from its header. Change any that is wrong; a column not used is left out."
      >
        {file.table.headers.map((h, i) => (
          <ColumnRow
            key={i}
            header={h}
            sample={sample(i)}
            role={roles[i] ?? "ignore"}
            onChange={(role) =>
              setRoles((rs) =>
                rs.map((r, j) =>
                  j === i
                    ? role
                    : r === role && role !== "ignore"
                      ? "ignore"
                      : r
                )
              )
            }
          />
        ))}
        {missing.map((role) => (
          <NoteRow key={role} tone="caution">
            Pick the column that holds the {roleLabel(role).toLowerCase()}.
          </NoteRow>
        ))}
      </Section>

      {orderAsked && (
        <Section
          header="Dates"
          footer="Every date in the file reads both ways, such as 03/04/2026."
        >
          <div className="flex flex-col gap-2 px-4 py-3">
            <span className="text-[15px]">Which comes first?</span>
            <SegmentedControl
              label="Which comes first"
              value={order ?? "none"}
              options={[
                { value: "dmy", label: "The day" },
                { value: "mdy", label: "The month" },
                ...(order ? [] : [{ value: "none", label: "Choose" }]),
              ]}
              onChange={(v) => {
                if (v !== "none") setOrder(v as DayOrder)
              }}
            />
          </div>
        </Section>
      )}

      {needsLab && (
        <Section
          footer={
            labColumn
              ? "For the rows that name no lab. Each lab becomes a source of its own, or the one of that name you already have."
              : "The file names no lab. Its results become a source of their own, or the one of that name you already have."
          }
        >
          <Field
            label="Lab"
            value={lab}
            onChange={setLab}
            placeholder="The lab's name"
          />
        </Section>
      )}

      {!missing.length && !order && (
        <p className="mb-7 px-4 text-[15px] text-secondary">
          Choose which comes first in the dates to see the plan.
        </p>
      )}

      {matchError && (
        <Section>
          <ProblemBox
            role="alert"
            title="Couldn't match the rows"
            message={matchError}
            action={{
              title: "Try Again",
              onClick: () => setPicks((p) => new Map(p)),
            }}
          />
        </Section>
      )}

      {(matching || (!matched && !missing.length && order)) && (
        <div className="mb-7 flex items-center justify-center gap-2 py-6 text-[15px] text-secondary">
          <Spinner /> Matching the rows…
        </div>
      )}

      {matched && !matching && (
        <>
          <Section header="The plan">
            {plan.map((p, i) => (
              <NoteRow key={i} tone="positive">
                {`${p.rows.length.toLocaleString("en-GB")} ${p.rows.length === 1 ? "result" : "results"} in ${p.reports.length} ${p.reports.length === 1 ? "report" : "reports"} from ${p.lab}`}
              </NoteRow>
            ))}
            {!plan.length && (
              <NoteRow tone="caution">Nothing is ticked.</NoteRow>
            )}
            {matched.length - planned > 0 && (
              <NoteRow>
                {`${(matched.length - planned).toLocaleString("en-GB")} left out`}
              </NoteRow>
            )}
          </Section>

          {[...groups.values()].map((rows) => {
            const first = rows[0]
            return (
              <Section
                key={`${first.lab}|${first.accession ?? first.day}`}
                header={[
                  first.at ? formatDate(first.at) : undefined,
                  first.lab,
                  first.accession ? `accession ${first.accession}` : undefined,
                ]
                  .filter(Boolean)
                  .join(", ")}
              >
                {rows.map((r) => (
                  <TickRow
                    key={r.line}
                    row={r}
                    ticked={ticked.has(r.line)}
                    onToggle={() =>
                      setToggled((t) =>
                        new Map(t).set(r.line, !ticked.has(r.line))
                      )
                    }
                  />
                ))}
              </Section>
            )
          })}

          {leftOut.length > 0 && (
            <Section
              header="Left out"
              footer="A test nothing knows can be matched by hand: pick the metric it is, and every row of that test follows."
            >
              {leftOut.map((r) => (
                <div
                  key={r.line}
                  className="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5"
                >
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="text-[17px] break-words">
                      {r.name ?? r.loinc ?? `Line ${r.line}`}
                    </span>
                    <span className="text-[13px] break-words text-secondary">
                      {`Line ${r.line}: ${r.problem}`}
                    </span>
                  </span>
                  {r.unmatched && (
                    <select
                      value={picks.get(pickKey(r)) ?? ""}
                      aria-label={`The metric ${r.name ?? "this test"} is`}
                      onChange={(e) =>
                        setPicks((p) => {
                          const next = new Map(p)
                          if (e.target.value)
                            next.set(pickKey(r), e.target.value)
                          else next.delete(pickKey(r))
                          return next
                        })
                      }
                      className="h-9 max-w-full rounded-[8px] bg-fill px-2 text-[15px] text-label outline-none focus-visible:ring-2 focus-visible:ring-accent"
                    >
                      <option value="">Pick a metric</option>
                      {labMetrics.map((m) => (
                        <option key={m.id} value={m.id}>
                          {index.get(m.id)?.name ?? m.name}
                        </option>
                      ))}
                    </select>
                  )}
                </div>
              ))}
            </Section>
          )}

          {startError && (
            <Section>
              <ProblemBox
                role="alert"
                title="Not imported"
                message={startError}
              />
            </Section>
          )}
          <div className="mb-7 flex flex-col items-stretch gap-2">
            <BarButton
              prominent
              disabled={!ready}
              onClick={() => void start()}
              className="w-full"
            >
              {starting
                ? "Importing…"
                : `Import ${planned.toLocaleString("en-GB")} ${planned === 1 ? "result" : "results"}`}
            </BarButton>
            <TextButton className="self-center" onClick={props.onBack}>
              Cancel
            </TextButton>
          </div>
        </>
      )}
    </>
  )
}
