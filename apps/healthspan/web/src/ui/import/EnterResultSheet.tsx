import {
  BarButton,
  Field,
  ProblemBox,
  Section,
  SelectRow,
  Sheet,
} from "@substrate/react"
import { useMemo, useState } from "react"
import { CATALOGUE } from "../../catalogue"
import { useBackend } from "../../data"
import { useMode } from "../../data/mode"
import type { ColumnRole } from "../../import/columns"
import { buildPlan } from "../../import/plan"
import { matchRows, pickKey, readRows } from "../../import/resolve"
import { unitLabel } from "../../lib/units"
import { go } from "../../route"

const FLAGS = [
  { value: "", label: "None" },
  { value: "H", label: "High" },
  { value: "L", label: "Low" },
  { value: "A", label: "Abnormal" },
]

const ROLES: ColumnRole[] = [
  "date",
  "name",
  "value",
  "unit",
  "low",
  "high",
  "flag",
  "lab",
]

function isoDay(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** One result typed in, written as an import of one row through the same
 * plan and runner as a file, so it can be undone like one. */
export function EnterResultSheet(props: {
  open: boolean
  onClose: () => void
}) {
  // Mounted afresh each time it opens, so it starts empty.
  return props.open ? <EnterResultForm onClose={props.onClose} /> : null
}

function EnterResultForm(props: { onClose: () => void }) {
  const { today } = useMode()
  const sources = useBackend().sources
  const metricsRead = sources.useLabMetrics()
  const labs = (sources.useSourceList().data ?? []).filter(
    (s) => s.class === "lab"
  )
  const actions = sources.useImportActions()
  const sorted = useMemo(
    () =>
      [...(metricsRead.data ?? [])].sort((a, b) =>
        a.name.localeCompare(b.name)
      ),
    [metricsRead.data]
  )
  const [metric, setMetric] = useState("")
  const [day, setDay] = useState(isoDay(today))
  const [value, setValue] = useState("")
  const [unit, setUnit] = useState("")
  const [low, setLow] = useState("")
  const [high, setHigh] = useState("")
  const [flag, setFlag] = useState("")
  const [lab, setLab] = useState(labs.length === 1 ? labs[0].name : "")
  const [problem, setProblem] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const chosen = sorted.find((m) => m.id === metric)
  const ready =
    !!chosen && !!day && value.trim() !== "" && lab.trim() !== "" && !saving

  const save = async () => {
    if (!chosen) return
    setSaving(true)
    setProblem(null)
    try {
      const table = {
        delimiter: "," as const,
        headers: ROLES,
        rows: [[day, chosen.name, value, unit, low, high, flag, lab]],
      }
      const rows = readRows(table, ROLES, "dmy", lab)
      const [row] = await matchRows(rows, {
        catalogue: CATALOGUE,
        metrics: [chosen],
        picks: new Map([[pickKey({ name: chosen.name }), chosen.id]]),
        convert: actions.convert,
        held: actions.held,
      })
      if (row.problem) {
        setProblem(row.problem)
        return
      }
      const plan = buildPlan([row], new Set([row.line]), {
        name: "Entered by hand",
        format: "manual",
      })
      const [id] = await actions.start(plan)
      props.onClose()
      if (id) go({ screen: "import", id })
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet
      open
      onClose={props.onClose}
      title="Enter a result"
      unsaved={value !== "" || metric !== ""}
      actions={
        <BarButton prominent disabled={!ready} onClick={() => void save()}>
          Save
        </BarButton>
      }
    >
      <Section>
        <SelectRow
          label="Test"
          value={metric}
          options={[
            { value: "", label: "Choose" },
            ...sorted.map((m) => ({ value: m.id, label: m.name })),
          ]}
          onChange={(id) => {
            setMetric(id)
            const m = sorted.find((x) => x.id === id)
            setUnit(m?.unit ?? "")
          }}
        />
        <Field label="Date taken" type="date" value={day} onChange={setDay} />
      </Section>
      <Section
        footer={
          chosen?.unit
            ? `Its metric is kept in ${unitLabel(chosen.unit) || chosen.unit}; another unit that converts to it is kept as written.`
            : "A bound such as <0.5, or words such as Not detected, are kept as written."
        }
      >
        <Field
          label="Value"
          value={value}
          onChange={setValue}
          placeholder="Such as 5.2, <0.5 or Not detected"
        />
        <Field label="Unit" value={unit} onChange={setUnit} />
      </Section>
      <Section header="The lab's range">
        <Field label="Low" value={low} onChange={setLow} />
        <Field label="High" value={high} onChange={setHigh} />
        <SelectRow
          label="Flag"
          value={flag}
          options={FLAGS}
          onChange={setFlag}
        />
      </Section>
      <Section footer="The lab becomes a source of its own, or the one of that name you already have.">
        <Field
          label="Lab"
          value={lab}
          onChange={setLab}
          placeholder={labs[0]?.name ?? "The lab's name"}
        />
      </Section>
      {problem && (
        <div className="overflow-hidden rounded-[10px] bg-card">
          <ProblemBox role="alert" title="Not saved" message={problem} />
        </div>
      )}
    </Sheet>
  )
}
