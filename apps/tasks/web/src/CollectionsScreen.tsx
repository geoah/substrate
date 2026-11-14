import {
  LargeTitle,
  NavBar,
  Row,
  Section,
  SelectRow,
  Toggle,
  cx,
  useSubstrate,
} from "@substrate/react"
import { Check, ChevronRight } from "lucide-react"
import { useTaskActions } from "./actions"
import { isOpen } from "./model"
import {
  grantWrites,
  kindLabel,
  ownTaskKind,
  propertyLabel,
  type Collection,
  type Reading,
} from "./sources"
import { openSettings, useTasks } from "./state"
import { words } from "./words"

/** Whether the owner asked for write access to a collection and the
 * approval left it out: the collection stays view only, said once. */
function refused(
  c: Collection,
  requested: readonly string[] | undefined,
  grant: readonly string[]
): boolean {
  return (
    !!requested?.includes(`write:${c.kind}`) &&
    !grantWrites(grant, c.kind, c.declaration.definition)
  )
}

/** A collection the grant does not write: the sentence, and Approve, which
 * opens the server's approval asking for `write:<kind>` beside the app's
 * request and every collection's scope, whether the collection is on or
 * not (one turned on from another device is view only here until then).
 * Settings → Collections shows it for the collections that are on; their
 * pages and their tasks' pages show it always. */
export function ApprovalNote(props: {
  collection: Collection
  /** Only where the collection is on. */
  whenOn?: boolean
  className?: string
}) {
  const { session, signInAgain } = useSubstrate()
  const c = props.collection
  if (!c.viewOnly || c.kind === ownTaskKind) return null
  if (props.whenOn && !c.enabled) return null
  const no = refused(c, session.requestedScopes, session.scopes)
  return (
    <div
      className={cx(
        "mb-4 flex flex-wrap items-center gap-3 rounded-[10px] bg-card px-4 py-3",
        props.className
      )}
    >
      <p className="min-w-[12rem] flex-1 text-[15px]">
        {no
          ? words.approval.refused(c.label.plural)
          : words.approval.ask(c.label.plural)}
      </p>
      <button
        type="button"
        onClick={() => void signInAgain([`write:${c.kind}`])}
        className="h-11 shrink-0 rounded-full bg-accent px-4 text-[15px] font-semibold text-white active:opacity-80"
      >
        {words.approval.approve}
      </button>
    </div>
  )
}

/** Settings → Collections: every candidate with its label, its full
 * reference, its counts and a switch; View only where the grant writes
 * nothing; and where new tasks are saved. */
export function CollectionsScreen(props: { onBack: () => void }) {
  const model = useTasks()
  const actions = useTaskActions()
  const { collections, newTasks } = model.sources
  const targets = collections.filter(
    (c) =>
      c.enabled && !c.viewOnly && (c.title !== null || c.kind === ownTaskKind)
  )
  const meta = (c: Collection) => {
    const parts: string[] = []
    if (c.viewOnly) parts.push(words.collections.viewOnly)
    if (c.enabled) {
      const items = model.tasks.filter((t) => t.kind === c.kind)
      parts.push(words.collections.open(items.filter(isOpen).length))
      const suggested = items.filter((t) => t.status === "proposed").length
      if (suggested) parts.push(words.collections.suggested(suggested))
    }
    if (c.group)
      parts.push(
        words.collections.groupedBy(
          propertyLabel(c.declaration.definition, c.group.property)
        )
      )
    return parts.join(" · ")
  }
  return (
    <div className="mx-auto w-full max-w-2xl pb-32">
      <NavBar onBack={props.onBack} backLabel="Settings" />
      <LargeTitle className="pb-1">{words.collections.title}</LargeTitle>
      <p className="px-4 pb-5 text-[15px] text-secondary">
        {words.collections.detail}
      </p>
      <div className="px-4">
        {collections.map((c) => (
          <ApprovalNote key={c.kind} collection={c} whenOn />
        ))}
        <Section
          header={words.collections.showFrom}
          footer={words.collections.footer}
        >
          {collections.map((c) => (
            <div
              key={c.kind}
              className="flex min-h-11 items-center gap-3 px-4 py-2.5"
            >
              <button
                type="button"
                onClick={() =>
                  openSettings({ page: "collection", kind: c.kind })
                }
                className="min-w-0 flex-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                <span className="block text-[17px]">{c.label.plural}</span>
                <span className="block font-mono text-[13px] break-all text-secondary">
                  {c.kind}
                </span>
                {meta(c) && (
                  <span className="block text-[13px] text-secondary">
                    {meta(c)}
                  </span>
                )}
              </button>
              <Toggle
                label={c.label.plural}
                checked={c.enabled}
                onChange={(enabled) =>
                  actions.setCollection(c.kind, { enabled })
                }
              />
              <button
                type="button"
                aria-label={`${c.label.plural}: how it reads`}
                onClick={() =>
                  openSettings({ page: "collection", kind: c.kind })
                }
                className="flex size-11 items-center justify-center text-tertiary"
              >
                <ChevronRight className="size-4" aria-hidden />
              </button>
            </div>
          ))}
        </Section>
        <Section header={words.collections.saveTo}>
          <SelectRow
            label={words.collections.newTasks}
            value={newTasks}
            options={(targets.some((c) => c.kind === newTasks)
              ? targets
              : [...targets, ...collections.filter((c) => c.kind === newTasks)]
            ).map((c) => ({ value: c.kind, label: c.label.plural }))}
            onChange={(kind) =>
              actions.setPreference({
                newTasks: kind === ownTaskKind ? null : kind,
              })
            }
          />
        </Section>
      </div>
    </div>
  )
}

const readings: { reading: Reading; type: string }[] = [
  { reading: "due", type: "datetime" },
  { reading: "dueDay", type: "date" },
  { reading: "priority", type: "enum" },
  { reading: "order", type: "decimal" },
]

const none = "-"

/** One collection: its label and full reference, Group by (each single
 * reference by its label with its target kind under it, and Nothing), how
 * its states read, and each field reading with what the app guessed. */
export function CollectionScreen(props: { kind: string; onBack: () => void }) {
  const model = useTasks()
  const actions = useTaskActions()
  const { session, signInAgain } = useSubstrate()
  const c = model.collection(props.kind)
  if (!c)
    return (
      <div className="mx-auto w-full max-w-2xl">
        <NavBar onBack={props.onBack} backLabel={words.collections.title} />
        <p className="px-8 pt-20 text-center text-[17px] text-secondary">
          {props.kind}
        </p>
      </div>
    )
  const definition = c.declaration.definition
  const props_ = (definition.properties ?? {}) as Record<
    string,
    { type?: unknown; repeated?: unknown }
  >
  const ofType = (type: string) =>
    Object.entries(props_)
      .filter(([, spec]) => spec.type === type && spec.repeated !== true)
      .map(([name]) => name)
  const { status } = c
  const open = status.values.filter(
    (v) =>
      !status.done.includes(v) &&
      !status.dropped.includes(v) &&
      !status.proposed.includes(v)
  )
  const choice = c.record?.properties.group
  const waiting = c.waitingGroup
  const asked =
    waiting && session.requestedScopes?.includes(`read:${waiting.kind}`)
  return (
    <div className="mx-auto w-full max-w-2xl pb-32">
      <NavBar onBack={props.onBack} backLabel={words.collections.title} />
      <LargeTitle className="pb-0">{c.label.plural}</LargeTitle>
      <p className="px-4 pb-5 font-mono text-[13px] break-all text-secondary">
        {c.kind}
      </p>
      <div className="px-4">
        <ApprovalNote collection={c} />
        {waiting && (
          <div className="mb-4 flex flex-wrap items-center gap-3 rounded-[10px] bg-card px-4 py-3">
            <p className="min-w-[12rem] flex-1 text-[15px]">
              {asked
                ? words.approval.readRefused(c.label.plural)
                : words.approval.readAsk(
                    kindLabel(waiting.kind, model.declaration(waiting.kind))
                      .plural
                  )}
            </p>
            <button
              type="button"
              onClick={() => void signInAgain([`read:${waiting.kind}`])}
              className="h-11 shrink-0 rounded-full bg-accent px-4 text-[15px] font-semibold text-white active:opacity-80"
            >
              {words.approval.approve}
            </button>
          </div>
        )}
        <Section header={words.collection.groupBy}>
          {c.references.map((r) => (
            <ChoiceRow
              key={r.property}
              title={r.label}
              detail={r.kind}
              mono
              checked={
                c.group?.property === r.property || choice === r.property
              }
              onClick={() =>
                actions.setCollection(c.kind, { group: r.property })
              }
            />
          ))}
          <ChoiceRow
            title={words.collection.nothing}
            detail={words.collection.nothingDetail}
            checked={!c.group && !waiting}
            onClick={() => actions.setCollection(c.kind, { group: none })}
          />
        </Section>
        <Section header={words.collection.states}>
          <StateRow
            label={words.collection.proposed}
            values={status.proposed}
          />
          <StateRow label={words.collection.open} values={open} />
          <StateRow label={words.collection.done} values={status.done} />
          <StateRow label={words.collection.dropped} values={status.dropped} />
        </Section>
        <Section header={words.collection.fields}>
          <Row
            label={words.collection.title}
            value={
              <span className="font-mono text-[15px]">
                {c.title ?? words.collection.none}
              </span>
            }
          />
          {readings.map(({ reading, type }) => {
            const options = ofType(type)
            const guess = c.guess[reading]
            if (guess && !options.includes(guess)) options.push(guess)
            return (
              <SelectRow
                key={reading}
                label={words.collection[reading]}
                value={c[reading] ?? none}
                options={[
                  { value: none, label: words.collection.none },
                  ...options.map((p) => ({
                    value: p,
                    label: p,
                  })),
                ]}
                onChange={(v) =>
                  actions.setCollection(c.kind, { [reading]: v })
                }
              />
            )
          })}
        </Section>
      </div>
    </div>
  )
}

function ChoiceRow(props: {
  title: string
  detail: string
  mono?: boolean
  checked: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={props.checked}
      onClick={props.onClick}
      className="flex min-h-11 w-full items-center gap-3 px-4 py-2.5 text-left active:bg-selected"
    >
      <span className="min-w-0 flex-1">
        <span className="block text-[17px]">{props.title}</span>
        <span
          className={cx(
            "block break-all text-secondary",
            props.mono ? "font-mono text-[13px]" : "text-[15px]"
          )}
        >
          {props.detail}
        </span>
      </span>
      {props.checked && <Check className="size-5 text-accent-text" />}
    </button>
  )
}

function StateRow(props: { label: string; values: string[] }) {
  return (
    <div className="flex min-h-11 flex-wrap items-center gap-2 px-4 py-2">
      <span className="text-[17px]">{props.label}</span>
      <span className="ml-auto flex flex-wrap justify-end gap-1.5">
        {props.values.length ? (
          props.values.map((v) => (
            <span
              key={v}
              className="rounded-full bg-fill px-2.5 py-0.5 font-mono text-[13px] text-secondary"
            >
              {v}
            </span>
          ))
        ) : (
          <span className="text-[15px] text-tertiary">
            {words.collection.none}
          </span>
        )}
      </span>
    </div>
  )
}
