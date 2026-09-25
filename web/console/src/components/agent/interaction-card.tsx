/** The agent's questions, inline in the thread that asked them. The card RESOLVES the
 * llm/interaction and renders its live state, exactly as the proposal card
 * does: a pending batch is a form (radio per single-select question,
 * checkboxes per multi, dismissal beside submit), a resolved one shows what
 * was chosen. The answer is ONE CAS'd patch performing the answering
 * transition with the answers aboard; the engine validates every selection
 * against the STORED option values and refuses partial batches, so this form
 * enforces completeness client-side only as a courtesy.
 *
 * Question prompts and option labels are MODEL-AUTHORED and render as plain
 * untrusted text; what the submit sends is stored option VALUES, never
 * anything typed or derived from a label. */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { MessageCircleQuestionIcon } from "lucide-react"

import { StateBadge } from "@/components/identity/state-badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { CORE_AUTHORITY, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { patchRecord, recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { cn } from "@/lib/utils"

const CARD =
  "flex flex-col gap-2 rounded-[10px] border border-border-strong bg-background px-3.5 py-3 text-sm"

// The collection segment is the kind name (decision 0033).
const INTERACTION_KIND = "interaction"

interface Option {
  value: string
  label?: string
  description?: string
}

interface Question {
  id: string
  prompt: string
  options: Option[]
  multi: boolean
}

function questionsOf(record: SubstrateRecord): Question[] {
  const raw = record.properties.questions
  if (!Array.isArray(raw)) return []
  const out: Question[] = []
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue
    const q = item as Record<string, unknown>
    const id = typeof q.id === "string" ? q.id : ""
    const prompt = typeof q.prompt === "string" ? q.prompt : ""
    if (!id || !prompt) continue
    const options: Option[] = []
    if (Array.isArray(q.options)) {
      for (const o of q.options) {
        if (typeof o !== "object" || o === null) continue
        const opt = o as Record<string, unknown>
        if (typeof opt.value !== "string" || !opt.value) continue
        options.push({
          value: opt.value,
          label: typeof opt.label === "string" ? opt.label : undefined,
          description:
            typeof opt.description === "string" ? opt.description : undefined,
        })
      }
    }
    out.push({ id, prompt, options, multi: q.multi === true })
  }
  return out
}

function answersOf(record: SubstrateRecord): Map<string, string[]> {
  const out = new Map<string, string[]>()
  const raw = record.properties.answers
  if (!Array.isArray(raw)) return out
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue
    const a = item as Record<string, unknown>
    if (typeof a.question !== "string") continue
    const selected = Array.isArray(a.selected)
      ? a.selected.filter((v): v is string => typeof v === "string")
      : []
    out.set(a.question, selected)
  }
  return out
}

export function InteractionCard({ id }: { id: string }) {
  const client = useQueryClient()
  const interaction = useQuery(
    recordQueryOptions(CORE_AUTHORITY, LLM_PACKAGE_NAME, INTERACTION_KIND, id)
  )
  const [picked, setPicked] = useState<Map<string, string[]>>(new Map())
  const [submitting, setSubmitting] = useState<"answer" | "dismiss" | null>(
    null
  )
  const [error, setError] = useState<string | null>(null)

  if (interaction.isPending) {
    return (
      <div className={CARD}>
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Spinner className="size-3" />
          Loading the questions…
        </span>
      </div>
    )
  }
  if (interaction.isError || !interaction.data) return null

  const record = interaction.data
  const state =
    typeof record.properties.state === "string"
      ? record.properties.state
      : "pending"
  const questions = questionsOf(record)
  const answered = answersOf(record)
  const pending = state === "pending"
  const complete = questions.every((q) => (picked.get(q.id)?.length ?? 0) > 0)

  function toggle(q: Question, value: string) {
    setPicked((prev) => {
      const next = new Map(prev)
      const cur = next.get(q.id) ?? []
      if (q.multi) {
        next.set(
          q.id,
          cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value]
        )
      } else {
        next.set(q.id, [value])
      }
      return next
    })
  }

  async function resolve(kind: "answer" | "dismiss") {
    setSubmitting(kind)
    setError(null)
    try {
      await patchRecord(
        CORE_AUTHORITY,
        LLM_PACKAGE_NAME,
        INTERACTION_KIND,
        id,
        {
          properties:
            kind === "answer"
              ? {
                  state: "answered",
                  answers: questions.map((q) => ({
                    question: q.id,
                    selected: picked.get(q.id) ?? [],
                  })),
                }
              : { state: "dismissed" },
          ifVersion: record.version,
        }
      )
      // The resolution wrote a system turn into THIS thread and resumed the
      // agent; one more sweep catches the reply without polling forever.
      await client.invalidateQueries()
      setTimeout(() => void client.invalidateQueries(), 4000)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      void client.invalidateQueries()
    } finally {
      setSubmitting(null)
    }
  }

  return (
    <div className={CARD} data-slot="interaction-card">
      <div className="flex flex-wrap items-center gap-2 font-medium">
        <MessageCircleQuestionIcon className="size-4 shrink-0 text-faint" />
        <span>
          {pending ? "A few questions before it goes on" : "Its questions"}
        </span>
        {!pending && (
          <StateBadge value={state} className="ml-auto text-[12.5px]" />
        )}
      </div>
      {questions.map((q) => (
        <fieldset key={q.id} className="flex flex-col gap-1">
          <legend className="mb-1 text-[13px] [overflow-wrap:anywhere]">
            {q.prompt}
          </legend>
          <div className="flex flex-col gap-0.5">
            {q.options.map((o) => {
              const chosen = pending
                ? (picked.get(q.id) ?? []).includes(o.value)
                : (answered.get(q.id) ?? []).includes(o.value)
              return (
                <label
                  key={o.value}
                  className={cn(
                    "flex cursor-pointer items-start gap-2 rounded-md px-2 py-1 text-[13px]",
                    pending && "hover:bg-hover",
                    !pending && chosen && "bg-ok-soft",
                    !pending && "cursor-default"
                  )}
                >
                  <input
                    type={q.multi ? "checkbox" : "radio"}
                    name={`${id}-${q.id}`}
                    value={o.value}
                    checked={chosen}
                    disabled={!pending || submitting !== null}
                    onChange={() => toggle(q, o.value)}
                    className="mt-0.5"
                  />
                  <span className="[overflow-wrap:anywhere]">
                    {o.label || o.value}
                    {o.description && (
                      <span className="text-muted-foreground">
                        {" "}
                        {o.description}
                      </span>
                    )}
                  </span>
                </label>
              )
            })}
          </div>
        </fieldset>
      ))}
      {error && (
        <p className="text-[12.5px] text-destructive">
          That didn’t go through: {error}
        </p>
      )}
      {pending && (
        <div className="flex items-center gap-1.5">
          <Button
            size="sm"
            disabled={!complete || submitting !== null}
            onClick={() => void resolve("answer")}
          >
            {submitting === "answer" && <Spinner className="size-3" />}
            Answer
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={submitting !== null}
            onClick={() => void resolve("dismiss")}
          >
            {submitting === "dismiss" && <Spinner className="size-3" />}
            Dismiss
          </Button>
        </div>
      )}
    </div>
  )
}
