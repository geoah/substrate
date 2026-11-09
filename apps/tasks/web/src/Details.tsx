import { Row, Section } from "@substrate/react"
import { Markdown } from "./markdown"
import type { TaskItem } from "./model"
import {
  nameWords,
  propertyLabel,
  recordTitle,
  referenceValue,
  type Collection,
} from "./sources"
import type { TasksModel } from "./state"
import { words } from "./words"

/** The properties a task's page shows in a place of their own: its status,
 * its title, each reading, its description and its link. */
function shownElsewhere(c: Collection): Set<string> {
  const out = new Set<string>([
    "title",
    "description",
    "url",
    c.status.property,
  ])
  for (const p of [
    c.title,
    c.due,
    c.dueDay,
    c.priority,
    c.order,
    c.group?.property,
  ])
    if (p) out.add(p)
  const template = c.declaration.definition.displayTemplate
  if (typeof template === "string")
    for (const m of template.matchAll(/\{([A-Za-z][A-Za-z0-9]*)\}/g))
      out.add(m[1])
  return out
}

/** A value as a row shows it: a reference by its record's title where the
 * token holds it, else its path; a list joined; a flag as Yes or No. */
export function valueText(v: unknown, model: TasksModel): string | undefined {
  if (v === null || v === undefined || v === "") return undefined
  const path = referenceValue(v)
  if (path && (typeof v === "object" || path.split("/").length > 3)) {
    const r = model.sources.held.get(path)
    return r ? recordTitle(r) : path
  }
  if (typeof v === "boolean") return v ? "Yes" : "No"
  if (typeof v === "string" || typeof v === "number") return String(v)
  if (Array.isArray(v)) {
    const parts = v.map((x) => valueText(x, model)).filter(Boolean)
    return parts.length ? parts.join(", ") : undefined
  }
  return JSON.stringify(v)
}

/** Details: every other property the record holds, read-only under its
 * declared label, then the collection by its label and full reference. */
export function DetailsSection(props: { task: TaskItem; model: TasksModel }) {
  const c = props.task.item.collection
  const skip = shownElsewhere(c)
  const definition = c.declaration.definition
  const declared = Object.keys(
    (definition.properties as Record<string, unknown> | undefined) ?? {}
  )
  const rows = declared
    .filter((p) => !skip.has(p))
    .map((p) => ({
      name: p,
      text: valueText(props.task.item.record.properties[p], props.model),
    }))
    .filter((r): r is { name: string; text: string } => !!r.text)
  return (
    <Section header={words.suggestion.details}>
      {rows.map((r) => (
        <Row
          key={r.name}
          label={propertyLabel(definition, r.name)}
          value={r.text}
        />
      ))}
      <Row
        label={words.suggestion.collection}
        value={
          <span className="flex flex-col items-end">
            <span>{c.label.plural}</span>
            <span className="font-mono text-[12px] break-all text-tertiary">
              {c.kind}
            </span>
          </span>
        }
      />
    </Section>
  )
}

/** The description under its property's declared label. */
export function DescriptionSection(props: { task: TaskItem; label?: string }) {
  const t = props.task
  if (!t.description) return null
  const c = t.item.collection
  return (
    <Section
      header={
        props.label ?? propertyLabel(c.declaration.definition, "description")
      }
    >
      <div className="px-4 py-3 text-[17px] leading-relaxed select-text">
        <Markdown text={t.description} />
      </div>
    </Section>
  )
}

/** A status value in words, as a fact. */
export function statusText(t: TaskItem): string | undefined {
  return t.item.value ? nameWords(t.item.value) : undefined
}
