import {
  BarButton,
  Field,
  ProblemBox,
  Section,
  SelectRow,
  Sheet,
} from "@substrate/react"
import { uuidv7 } from "@substrate/client"
import { useMemo, useState } from "react"
import { useBackend } from "../../data"
import { useMode } from "../../data/mode"
import { unitLabel } from "../../lib/units"
import { go } from "../../route"
import { useImportContext } from "./context"

const FLAGS = [
  { value: "", label: "None" },
  { value: "H", label: "High" },
  { value: "L", label: "Low" },
  { value: "A", label: "Abnormal" },
]

function isoDay(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** One result typed in, read by the hand entry's importer into a plan of
 * one report and one result, and written by the same runner as a file, so
 * it can be undone like one. */
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
  const base = useImportContext()
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
    !!chosen &&
    !!base &&
    !!day &&
    value.trim() !== "" &&
    lab.trim() !== "" &&
    !saving

  const save = async () => {
    if (!chosen || !base) return
    setSaving(true)
    setProblem(null)
    try {
      const { MANUAL_ANSWERS, NO_INPUT, errorWords, manual, readPlan } =
        await import("./manualEntry")
      const out = await readPlan(manual, NO_INPUT, {
        ...base,
        // The test picked is one of the lab metrics, whichever list the
        // context holds.
        metrics: [chosen, ...base.metrics.filter((m) => m.id !== chosen.id)],
        answers: {
          [MANUAL_ANSWERS.metric]: chosen.id,
          [MANUAL_ANSWERS.day]: day,
          [MANUAL_ANSWERS.value]: value,
          [MANUAL_ANSWERS.unit]: unit,
          [MANUAL_ANSWERS.low]: low,
          [MANUAL_ANSWERS.high]: high,
          [MANUAL_ANSWERS.flag]: flag,
          [MANUAL_ANSWERS.lab]: lab,
        },
      })
      if ("error" in out) {
        setProblem(errorWords(out.error))
        return
      }
      const id = uuidv7()
      const [staged] = await actions.start([
        { id, plan: out.plan, lab: lab.trim() },
      ])
      if (staged?.state !== "planned") {
        setProblem(
          staged?.state === "failed"
            ? staged.words
            : "It could not be sent to your repository. Try again."
        )
        return
      }
      props.onClose()
      go({ screen: "import", id })
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
