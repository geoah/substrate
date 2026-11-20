import { BarButton, Field, ProblemBox, Section, Sheet } from "@substrate/react"
import { Check } from "lucide-react"
import { useState } from "react"
import { useBackend } from "../../data"
import type { SourceSummary } from "../../data/sources"
import type { SessionKind } from "../../sources/groups"
import { classWord } from "../../sources/list"

/** Picks two or more sources that are one device, names it, and links
 * them: a metric read then takes that device's values once, and the night
 * and workout orders keep the linked sources together. */
export function LinkSheet(props: LinkProps & { open: boolean }) {
  // Mounted afresh each time it opens, so it starts empty.
  return props.open ? <LinkForm {...props} /> : null
}

interface LinkProps {
  onClose: () => void
  sources: SourceSummary[]
  /** The night and workout orders shown, which the link moves together. */
  orders: Record<SessionKind, string[]>
  /** A source to start with picked. */
  start?: string
}

function LinkForm(props: LinkProps) {
  const writes = useBackend().sources.useSourceWrites()
  const [picked, setPicked] = useState<string[]>(
    props.start ? [props.start] : []
  )
  const [name, setName] = useState("")
  const [problem, setProblem] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const toggle = (id: string) =>
    setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))
  const ready = picked.length >= 2 && name.trim() !== "" && !saving

  const link = async () => {
    setSaving(true)
    const done = await writes.link(name, picked, props.orders)
    setSaving(false)
    if (done.ok) props.onClose()
    else setProblem(done.words)
  }

  return (
    <Sheet
      open
      onClose={props.onClose}
      title="One device"
      unsaved={picked.length > 0 || name !== ""}
      actions={
        <BarButton prominent disabled={!ready} onClick={() => void link()}>
          Link
        </BarButton>
      }
    >
      <p className="mb-4 px-1 text-[15px] text-secondary">
        Pick the sources that are the same device, such as a scale synced to its
        maker's app and to a phone. Every app reading a metric then counts that
        device's values once, from the one ranked highest.
      </p>
      <Section>
        <Field
          label="The device's name"
          value={name}
          onChange={setName}
          placeholder="Such as Bathroom scale"
          autoFocus
        />
      </Section>
      <Section header="Sources" footer="Pick two or more.">
        {props.sources.map((s) => {
          const on = picked.includes(s.id)
          return (
            <button
              key={s.id}
              type="button"
              role="checkbox"
              aria-checked={on}
              onClick={() => toggle(s.id)}
              className="flex min-h-11 w-full items-center gap-3 px-4 py-2.5 text-left active:bg-selected"
            >
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="text-[17px] break-words">{s.name}</span>
                <span className="text-[13px] text-secondary">
                  {classWord(s.class)}
                </span>
              </span>
              <span
                aria-hidden
                className={
                  on
                    ? "flex size-6 shrink-0 items-center justify-center rounded-full bg-accent text-white"
                    : "size-6 shrink-0 rounded-full ring-1 ring-separator"
                }
              >
                {on && <Check className="size-4" />}
              </span>
            </button>
          )
        })}
      </Section>
      {problem && (
        <div className="overflow-hidden rounded-[10px] bg-card">
          <ProblemBox
            role="alert"
            title="The link was not written"
            message={problem}
          />
        </div>
      )}
    </Sheet>
  )
}
