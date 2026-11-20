import { useState } from "react"
import {
  capitalize,
  formatBytes,
  formatDate,
  formatDateTime,
} from "../../lib/format"
import {
  durationDays,
  filesOf,
  type ClinicalRow,
  type StoredFile,
} from "../../record/read"
import { Badge } from "../Badge"
import { Markdown } from "./Markdown"
import { Facts, RecordRow, RowDate } from "./parts"

export type OpenFile = (file: StoredFile) => Promise<void>

const CHANNELS: Record<string, string> = {
  inperson: "In person",
  telephone: "Telephone",
  video: "Video",
  online: "Online",
  home: "Home visit",
}

/** An enum value in words: `inperson` -> `In person`, `enteredinerror` ->
 * `Entered in error`. */
export function word(v: unknown): string | undefined {
  if (typeof v !== "string" || !v.trim()) return undefined
  if (CHANNELS[v]) return CHANNELS[v]
  if (v === "enteredinerror") return "Entered in error"
  if (v === "unabletoassess") return "Unable to assess"
  if (v === "checkedin") return "Checked in"
  if (v === "onhold") return "On hold"
  if (v === "fulfilled") return "Attended"
  if (v === "noshow") return "Did not attend"
  return capitalize(v.replace(/[-_]/g, " "))
}

export function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() ? v : undefined
}

/** Facts in a sentence each: `Letter. Dr Example. Demo Surgery`. */
export function sentences(...parts: (string | undefined)[]): string {
  return parts.filter(Boolean).join(". ")
}

/** `PDF, 84 kB`. */
function fileFacts(f: StoredFile): string {
  const type = f.mediaType?.split("/")[1]?.toUpperCase()
  return [type, f.size !== undefined ? formatBytes(f.size) : undefined]
    .filter(Boolean)
    .join(", ")
}

/** The New badge and the clinical date. */
function newMeta(r: ClinicalRow, isNew: boolean) {
  return (
    <>
      {isNew && <Badge label="New" tone="info" />}
      <RowDate at={r.at} />
    </>
  )
}

/** Opens a stored file, busy while it does, its error said in place. */
function FileButton(props: {
  file: StoredFile
  label: string
  onOpenFile: OpenFile
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const open = async () => {
    setBusy(true)
    setError(undefined)
    try {
      await props.onOpenFile(props.file)
    } catch (e) {
      setError(e instanceof Error ? e.message : "Opening the file failed.")
    } finally {
      setBusy(false)
    }
  }
  return (
    <span className="inline-flex flex-col items-start gap-1">
      <button
        type="button"
        onClick={() => void open()}
        disabled={busy}
        aria-busy={busy || undefined}
        className="inline-flex min-h-9 items-center rounded-full bg-fill px-3 text-[15px] text-accent-text active:bg-selected disabled:opacity-60"
      >
        {busy ? "Opening…" : props.label}
      </button>
      {error && (
        <span role="alert" className="text-danger-ink text-[13px]">
          {error}
        </span>
      )}
    </span>
  )
}

function Files(props: {
  files: StoredFile[]
  onOpenFile?: OpenFile
  labelFor: (f: StoredFile, i: number) => string
}) {
  const n = props.files.length
  if (!n) return null
  if (!props.onOpenFile)
    return (
      <p className="text-[13px] text-secondary">
        {`${n === 1 ? "A file" : `${n} files`}, not kept in the demo`}
      </p>
    )
  const onOpenFile = props.onOpenFile
  return (
    <div className="flex flex-wrap gap-2">
      {props.files.map((f, i) => (
        <FileButton
          key={`${f.digest}-${i}`}
          file={f}
          label={props.labelFor(f, i)}
          onOpenFile={onOpenFile}
        />
      ))}
    </div>
  )
}

/** A document: its words unfold, its file opens. */
export function DocumentRow(props: {
  doc: ClinicalRow
  org: string
  isNew: boolean
  isOpen: boolean
  onOpenFile?: OpenFile
}) {
  const { doc } = props
  const files = filesOf(doc.p.file).slice(0, 1)
  const text = str(doc.p.text)
  const notes = str(doc.p.notes)
  const body =
    text || notes ? (
      <div className="flex flex-col gap-2">
        {notes && <p className="text-secondary">{notes}</p>}
        {text && <Markdown text={text} />}
      </div>
    ) : undefined
  const [expanded, setExpanded] = useState(props.isOpen && !!body)
  return (
    <RecordRow
      id={`row-${doc.kind}-${doc.id}`}
      highlighted={props.isOpen}
      title={doc.title}
      description={sentences(str(doc.p.type), str(doc.p.author), props.org)}
      meta={newMeta(doc, props.isNew)}
      body={
        files.length ? (
          <Files
            files={files}
            onOpenFile={props.onOpenFile}
            labelFor={(f) => `Open (${fileFacts(f) || "file"})`}
          />
        ) : undefined
      }
      detail={body}
      expanded={expanded}
      onExpandedChange={setExpanded}
    />
  )
}

/** A message: its body and attachments unfold. */
export function MessageRow(props: {
  message: ClinicalRow
  org: string
  isNew: boolean
  isOpen: boolean
  onOpenFile?: OpenFile
}) {
  const m = props.message
  const files = filesOf(m.p.attachments)
  const sender = str(m.p.sender)
  const [expanded, setExpanded] = useState(props.isOpen)
  return (
    <RecordRow
      id={`row-${m.kind}-${m.id}`}
      highlighted={props.isOpen}
      title={m.title}
      description={sentences(
        sender,
        props.org === sender ? undefined : props.org,
        files.length
          ? `${files.length} attachment${files.length > 1 ? "s" : ""}`
          : undefined
      )}
      meta={newMeta(m, props.isNew)}
      expanded={expanded}
      onExpandedChange={setExpanded}
      detail={
        <div className="flex flex-col gap-2">
          <Markdown text={str(m.p.body) ?? "No message text."} />
          <Files
            files={files}
            onOpenFile={props.onOpenFile}
            labelFor={(f, i) =>
              `Open attachment ${files.length > 1 ? `${i + 1} ` : ""}(${fileFacts(f) || "file"})`
            }
          />
        </div>
      }
    />
  )
}

export function AllergyRow(props: { allergy: ClinicalRow; org: string }) {
  const a = props.allergy
  const reactions = (
    Array.isArray(a.p.reactions) ? (a.p.reactions as unknown[]) : []
  )
    .map((r) => {
      const o = (r ?? {}) as Record<string, unknown>
      const what = str(o.manifestation) ?? str(o.description)
      const severity = str(o.severity)
      return what ? `${what}${severity ? ` (${severity})` : ""}` : undefined
    })
    .filter(Boolean)
  const status = word(a.p.clinicalStatus)
  const verification =
    a.p.verificationStatus === "confirmed"
      ? undefined
      : word(a.p.verificationStatus)
  const notes = str(a.p.notes)
  return (
    <RecordRow
      title={a.title}
      description={sentences(
        [word(a.p.type), str(a.p.category)].filter(Boolean).join(", ") ||
          undefined,
        reactions.length ? `Reaction: ${reactions.join(", ")}` : undefined,
        a.p.criticality === "high"
          ? "High risk of a severe reaction"
          : undefined,
        verification,
        props.org
      )}
      meta={
        <>
          {status && (
            <Badge
              label={status}
              tone={a.p.clinicalStatus === "active" ? "info" : "neutral"}
            />
          )}
          <RowDate at={a.at} />
        </>
      }
      detail={notes ? <p className="text-secondary">{notes}</p> : undefined}
    />
  )
}

export function AppointmentRow(props: {
  appt: ClinicalRow
  org: string
  upcoming: boolean
}) {
  const a = props.appt
  const when = a.at ? formatDateTime(a.at, str(a.p.endsAt)) : undefined
  const people = (
    Array.isArray(a.p.participants) ? (a.p.participants as unknown[]) : []
  )
    .map((x) => str((x as Record<string, unknown> | null)?.name))
    .filter((x): x is string => !!x)
  const facts = [
    { label: "Where", value: str(a.p.location) },
    { label: "With", value: people.length ? people.join(", ") : undefined },
    { label: "Phone", value: str(a.p.phone) },
    { label: "Clinic", value: str(a.p.service) },
    { label: "Reason", value: str(a.p.reason) },
    { label: "Notes", value: str(a.p.notes) },
  ].filter((f): f is { label: string; value: string } => !!f.value)
  const status = word(a.p.status)
  return (
    <RecordRow
      title={a.title}
      description={sentences(
        [when, word(a.p.channel)].filter(Boolean).join(", ") || undefined,
        str(a.p.specialty),
        props.org
      )}
      meta={
        status ? (
          <Badge label={status} tone={props.upcoming ? "info" : "neutral"} />
        ) : undefined
      }
      detail={facts.length ? <Facts items={facts} /> : undefined}
    />
  )
}

export function ReferralRow(props: { referral: ClinicalRow; org: string }) {
  const r = props.referral
  const wait = durationDays(r.p.typicalWait)
  const treatment = str(r.p.estimatedTreatmentAt)
  const status = word(r.p.status)
  const facts = [
    wait !== undefined
      ? { label: "Typical wait", value: `${Math.round(wait / 7)} weeks` }
      : undefined,
    treatment
      ? { label: "Estimated treatment", value: formatDate(treatment) }
      : undefined,
  ].filter((f): f is { label: string; value: string } => !!f)
  return (
    <RecordRow
      title={r.title}
      description={sentences(
        r.at ? `Referred ${formatDate(r.at)}` : undefined,
        str(r.p.specialty) === r.title ? undefined : str(r.p.specialty),
        props.org
      )}
      meta={
        status ? (
          <Badge
            label={status}
            tone={r.p.status === "active" ? "info" : "neutral"}
          />
        ) : undefined
      }
      body={facts.length ? <Facts items={facts} /> : undefined}
    />
  )
}

export function ConsultationRow(props: {
  encounter: ClinicalRow
  org: string
  isOpen: boolean
}) {
  const e = props.encounter
  const [expanded, setExpanded] = useState(props.isOpen)
  return (
    <RecordRow
      id={`row-${e.kind}-${e.id}`}
      highlighted={props.isOpen}
      title={e.title}
      description={sentences(
        str(e.p.type) === e.title ? undefined : str(e.p.type),
        str(e.p.location),
        props.org
      )}
      meta={<RowDate at={e.at} />}
      expanded={expanded}
      onExpandedChange={setExpanded}
      detail={<Markdown text={str(e.p.text) ?? "No notes."} />}
    />
  )
}
