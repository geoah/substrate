import {
  ActionRow,
  BarButton,
  Field,
  ProblemBox,
  Section,
  SelectRow,
  Spinner,
  cx,
} from "@substrate/react"
import { describe, uuidv7 } from "@substrate/client"
import { useEffect, useMemo, useRef, useState } from "react"
import { useBackend } from "../../data"
import type {
  Context,
  Importer,
  Input,
  Plan,
  Question,
} from "../../import/importers/types"
import { go } from "../../route"
import { metricIndex } from "../../sources/list"
import { TextButton } from "../IconButton"
import { PlanPreview } from "./PlanPreview"
import { useImportContext } from "./context"
import { errorWords, questionsOf, readPlan } from "./importers"
import { runWords } from "./words"

/** Every time zone the browser knows, for a question that asks one. */
function timeZones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone")
  } catch {
    return ["Europe/London", "UTC"]
  }
}

function QuestionField(props: {
  q: Question
  value: string
  onChange: (v: string) => void
}) {
  const { q } = props
  if (q.kind === "timezone") {
    const zones = timeZones()
    return (
      <SelectRow
        label={q.label}
        value={props.value}
        options={[
          ...(props.value ? [] : [{ value: "", label: "Choose" }]),
          ...zones.map((z) => ({ value: z, label: z.replace(/_/g, " ") })),
        ]}
        onChange={props.onChange}
      />
    )
  }
  return (
    <Field
      label={q.label}
      value={props.value}
      onChange={props.onChange}
      placeholder={q.suggested ?? (q.required ? "Required" : "Optional")}
    />
  )
}

/** A provider's files read through its importer: its questions asked, its
 * plan previewed (records by kind, documents, what was left out and why),
 * then Import: the plan staged as an import, then run. */
export function ProviderImport(props: {
  input: Input
  importer: Importer
  onBack: () => void
}) {
  const { input, importer } = props
  const sources = useBackend().sources
  const actions = sources.useImportActions()
  const metrics = metricIndex(sources.useMetrics().data ?? [])
  const base = useImportContext()
  const [questions, setQuestions] = useState<readonly Question[]>()
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [reading, setReading] = useState(false)
  const [plan, setPlan] = useState<Plan>()
  const [problem, setProblem] = useState<string>()
  const [startId, setStartId] = useState<string>()
  const [starting, setStarting] = useState(false)
  const run = sources.useRunState(startId ?? "")
  const abort = useRef<AbortController | null>(null)

  const ctx = useMemo(
    (): Context | undefined => (base ? { ...base, answers } : undefined),
    [base, answers]
  )

  const read = async (c: Context) => {
    abort.current?.abort()
    const controller = new AbortController()
    abort.current = controller
    setReading(true)
    setProblem(undefined)
    setPlan(undefined)
    try {
      const out = await readPlan(importer, input, c, controller.signal)
      if (controller.signal.aborted) return
      if ("plan" in out) setPlan(out.plan)
      else if (out.error._tag === "NeedsAnswers") {
        const asked = out.error.questions
        setQuestions((qs) => [
          ...(qs ?? []),
          ...asked.filter((q) => !(qs ?? []).some((x) => x.key === q.key)),
        ])
        setProblem(errorWords(out.error))
      } else setProblem(errorWords(out.error))
    } catch (e) {
      if (!controller.signal.aborted) setProblem(describe(e))
    } finally {
      if (abort.current === controller) setReading(false)
    }
  }

  // The questions first, their suggestions filled in; with none, the plan
  // at once.
  const asked = useRef(false)
  useEffect(() => {
    if (!base || asked.current) return
    asked.current = true
    const c: Context = { ...base, answers: {} }
    questionsOf(importer, input, c).then(
      (qs) => {
        const filled: Record<string, string> = {}
        for (const q of qs) if (q.suggested) filled[q.key] = q.suggested
        setAnswers(filled)
        setQuestions(qs)
        if (!qs.length) void read({ ...base, answers: filled })
      },
      (e: unknown) => setProblem(describe(e))
    )
    // `read` closes over this screen's input and importer alone.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [base, importer, input])

  useEffect(() => () => abort.current?.abort(), [])

  const missing = (questions ?? []).filter(
    (q) => q.required && !answers[q.key]?.trim()
  )

  const start = async () => {
    if (!plan) return
    const id = uuidv7()
    setStartId(id)
    setStarting(true)
    setProblem(undefined)
    try {
      const [out] = await actions.start([{ id, plan }])
      if (out?.state === "planned") go({ screen: "import", id })
      else
        setProblem(
          out?.state === "failed"
            ? out.words
            : "It could not be sent to your repository. Try again."
        )
    } catch (e) {
      setProblem(describe(e))
    } finally {
      setStarting(false)
    }
  }

  const progress = startId ? runWords(run) : undefined

  return (
    <>
      <Section>
        <div className="flex flex-col gap-0.5 px-4 py-2.5">
          <span className="text-[17px] break-words">{importer.name}</span>
          <span className="text-[15px] break-words text-secondary">
            {input.name}
          </span>
        </div>
        <ActionRow onClick={props.onBack}>Choose Something Else…</ActionRow>
      </Section>

      {!questions && !problem && (
        <div className="mb-7 flex items-center justify-center gap-2 py-6 text-[15px] text-secondary">
          <Spinner /> Reading what it needs…
        </div>
      )}

      {questions && questions.length > 0 && (
        <Section
          header="What the files do not say"
          footer={
            questions.find((q) => q.help)
              ? questions
                  .filter((q) => q.help)
                  .map((q) => q.help)
                  .join(" ")
              : "Asked once, and kept on the record it names."
          }
        >
          {questions.map((q) => (
            <QuestionField
              key={q.key}
              q={q}
              value={answers[q.key] ?? ""}
              onChange={(v) => setAnswers((a) => ({ ...a, [q.key]: v }))}
            />
          ))}
          <div className="px-4 py-3">
            <BarButton
              disabled={!ctx || reading || missing.length > 0}
              onClick={() => ctx && void read(ctx)}
            >
              {plan ? "Read Again" : "Read the Files"}
            </BarButton>
          </div>
        </Section>
      )}

      {problem && (
        <Section>
          <ProblemBox role="alert" title="Not read" message={problem} />
        </Section>
      )}

      {reading && (
        <div className="mb-7 flex flex-col items-center gap-3 py-6 text-[15px] text-secondary">
          <span className="flex items-center gap-2">
            <Spinner /> Reading the files…
          </span>
          <TextButton onClick={() => abort.current?.abort()}>Stop</TextButton>
        </div>
      )}

      {plan && !reading && (
        <>
          <PlanPreview plan={plan} metrics={metrics} />
          {progress && (
            <p
              role="status"
              className={cx(
                "mb-4 px-4 text-[15px] break-words",
                run.running ? "text-info-ink" : "text-secondary"
              )}
            >
              {progress}
            </p>
          )}
          <div className="mb-7 flex flex-col items-stretch gap-2">
            <BarButton
              prominent
              disabled={starting || !plan.records.length}
              onClick={() => void start()}
              className="w-full"
            >
              {starting
                ? "Importing…"
                : `Import ${plan.records.length.toLocaleString("en-GB")} ${plan.records.length === 1 ? "record" : "records"}`}
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
