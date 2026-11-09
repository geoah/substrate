import { cx } from "@substrate/react"
import { ExternalLink } from "lucide-react"
import { useId, type ReactNode } from "react"
import type { Owner } from "../../data/profile"
import type { SourceSummary } from "../../data/sources"
import type { Read } from "../../data/read"
import { KIND, type Profile, type Sex } from "../../kinds"
import { formatDate } from "../../lib/format"
import { birthDay } from "../../profile/owner"
import { ToggleGroup } from "../ToggleGroup"
import { SkeletonRow } from "../Skeleton"

export const SEX_WORDS: Record<Sex, string> = {
  female: "Female",
  male: "Male",
}

/** The console's pages this app sends the owner to. */
export const CONSOLE = {
  ownerName: "/settings/name",
  providers: "/providers",
  contact: (id: string) =>
    `/data/${KIND.contact.split("/").map(encodeURIComponent).join("/")}/${encodeURIComponent(id)}`,
}

/** A link to a page of the owner's console, opening beside the app; in the
 * demo, which has no console, the words alone. */
export function ConsoleLink(props: {
  link: ((path: string) => string) | undefined
  path: string
  children: ReactNode
  className?: string
  /** Where there is no console (the demo), nothing rather than the
   * words. */
  optional?: boolean
}) {
  if (!props.link)
    return props.optional ? null : (
      <span className={props.className}>{props.children}</span>
    )
  return (
    <a
      href={props.link(props.path)}
      target="_blank"
      rel="noreferrer"
      className={cx(
        "inline-flex items-center gap-1 text-accent-text underline decoration-accent-text/40 underline-offset-2",
        props.className
      )}
    >
      {props.children}
      <ExternalLink className="size-3.5 shrink-0" aria-hidden />
      <span className="sr-only">(opens the console)</span>
    </a>
  )
}

/** A value of the profile: its label and value, and under them where it
 * came from and what to do about it. */
export function ValueRow(props: {
  label: string
  value?: string
  /** Where it came from, and how it is changed. */
  note?: ReactNode
}) {
  return (
    <div className="px-4 py-2.5">
      <div className="flex flex-wrap items-baseline gap-x-3">
        <span className="text-[17px]">{props.label}</span>
        <span
          className={cx(
            "ml-auto text-[17px] break-words",
            props.value ? "text-label" : "text-secondary"
          )}
        >
          {props.value ?? "Not set"}
        </span>
      </div>
      {props.note && (
        <p className="mt-0.5 text-[13px] break-words text-secondary">
          {props.note}
        </p>
      )}
    </div>
  )
}

/** Where the owner's birth date came from, in words. */
export function birthDateNote(owner: Owner): string | undefined {
  if (owner.birthDate?.origin === "contact") return "From your contact"
  if (owner.birthDate?.origin === "profile") return "Kept by Healthspan"
  if (owner.contact?.birthday && !owner.contact.birthday.year)
    return "Your contact has a birthday without a year"
  return undefined
}

/** A date typed on a card row: its label above it, its help or its
 * problem under it. */
export function DateField(props: {
  label: string
  value: string
  onChange: (v: string) => void
  max?: string
  help?: ReactNode
  problem?: string
}) {
  const id = useId()
  return (
    <div className="px-4 py-2.5">
      <label htmlFor={id} className="block text-[13px] text-secondary">
        {props.label}
      </label>
      <input
        id={id}
        type="date"
        value={props.value}
        max={props.max}
        onChange={(e) => props.onChange(e.target.value)}
        aria-invalid={props.problem ? true : undefined}
        aria-describedby={`${id}-help`}
        className="mt-0.5 h-9 w-full max-w-60 rounded-[8px] bg-fill px-2 text-[17px] outline-none focus-visible:ring-2 focus-visible:ring-accent"
      />
      <p
        id={`${id}-help`}
        className={cx(
          "mt-1 text-[13px]",
          props.problem ? "text-danger-ink" : "text-secondary"
        )}
        role={props.problem ? "alert" : undefined}
      >
        {props.problem ?? props.help}
      </p>
    </div>
  )
}

/** The reference basis: which sex's curves and targets apply, said as
 * that and not as who the owner is. */
export function BasisField(props: {
  value: Sex | undefined
  onChange: (v: Sex | undefined) => void
  /** Offers "Not set", to clear it. */
  clearable?: boolean
  /** Where the value in use came from. */
  note?: string
}) {
  const options: { value: Sex | "none"; label: string }[] = [
    { value: "female", label: "Female" },
    { value: "male", label: "Male" },
    ...(props.clearable ? [{ value: "none" as const, label: "Not set" }] : []),
  ]
  const id = useId()
  return (
    <div className="px-4 py-2.5">
      <p id={id} className="text-[13px] text-secondary">
        Reference basis
      </p>
      <ToggleGroup
        className="mt-1"
        label="Reference basis"
        aria-describedby={`${id}-help`}
        isExclusive
        options={options}
        value={props.value ? [props.value] : props.clearable ? ["none"] : []}
        onChange={([v]) => props.onChange(v === "none" ? undefined : v)}
      />
      <p id={`${id}-help`} className="mt-1.5 text-[13px] text-secondary">
        {props.note && `${props.note}. `}
        Which reference curves the age score uses and which targets the labs
        apply, such as VO2 max, resting heart rate and lean mass. It is not a
        statement of identity: choose the ranges you want to be measured
        against.
      </p>
    </div>
  )
}

/** A value a patient registration holds, offered with the owner's yes:
 * never taken unasked. */
export function Suggestion(props: {
  owner: Owner
  what: string
  onUse: () => void
  used?: boolean
}) {
  const by = props.owner.suggestedBy?.service
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5">
      <p className="min-w-[min(100%,14rem)] flex-1 text-[15px] break-words text-secondary">
        {`Your patient registration${by ? ` with ${by}` : ""} records ${props.what}.`}
      </p>
      <button
        type="button"
        onClick={props.onUse}
        disabled={props.used}
        className="inline-flex min-h-9 shrink-0 items-center rounded-full bg-fill px-3 text-[15px] text-accent-text active:bg-selected disabled:text-secondary"
      >
        {props.used ? "Used" : "Use it"}
      </button>
    </div>
  )
}

/** The draft's filled fields against the row another device saved first:
 * both shown, the owner picks. */
export function ConflictChoice(props: {
  mine: { birthDate?: string | null; sex?: Sex | null }
  theirs: Profile
  onKeepMine: () => void
  onUseTheirs: () => void
  busy?: boolean
}) {
  const rows: { label: string; mine: string; theirs: string }[] = []
  const date = (v?: string | null) => (v ? formatDate(birthDay(v)) : "Not set")
  const sex = (v?: Sex | null) => (v ? SEX_WORDS[v] : "Not set")
  if (
    props.mine.birthDate &&
    props.theirs.birthDate &&
    props.mine.birthDate !== props.theirs.birthDate
  )
    rows.push({
      label: "Birth date",
      mine: date(props.mine.birthDate),
      theirs: date(props.theirs.birthDate),
    })
  if (props.mine.sex && props.theirs.sex && props.mine.sex !== props.theirs.sex)
    rows.push({
      label: "Reference basis",
      mine: sex(props.mine.sex),
      theirs: sex(props.theirs.sex),
    })
  return (
    <div role="alert" className="px-4 py-3">
      <p className="text-warn-ink text-[15px] font-semibold">
        Saved from another device
      </p>
      <p className="mt-1 text-[15px]">
        Your profile was saved on another device first, with other values.
        Choose which to keep.
      </p>
      <dl className="mt-2 flex flex-col gap-2">
        {rows.map((r) => (
          <div key={r.label}>
            <dt className="text-[13px] text-secondary">{r.label}</dt>
            <dd className="text-[15px]">
              <span className="block">{`Here: ${r.mine}`}</span>
              <span className="block">{`Other device: ${r.theirs}`}</span>
            </dd>
          </div>
        ))}
      </dl>
      <div className="mt-2 flex flex-wrap gap-x-5 gap-y-1">
        <button
          type="button"
          onClick={props.onKeepMine}
          disabled={props.busy}
          className="min-h-11 text-[15px] font-semibold text-accent-text disabled:opacity-40"
        >
          Keep mine
        </button>
        <button
          type="button"
          onClick={props.onUseTheirs}
          disabled={props.busy}
          className="min-h-11 text-[15px] text-accent-text disabled:opacity-40"
        >
          Use the other device's
        </button>
      </div>
    </div>
  )
}

const CLASS_WORDS: Record<string, string> = {
  watch: "Watch",
  ring: "Ring",
  scale: "Scale",
  lab: "Lab",
  service: "Service",
  app: "App",
  manual: "Entered by hand",
}

function holds(s: SourceSummary): string | undefined {
  const parts = [
    s.metrics.length
      ? `${s.metrics.length} ${s.metrics.length === 1 ? "measure" : "measures"}`
      : undefined,
    s.nights ? `${s.nights} ${s.nights === 1 ? "night" : "nights"}` : undefined,
    s.workouts
      ? `${s.workouts} ${s.workouts === 1 ? "workout" : "workouts"}`
      : undefined,
    s.reports
      ? `${s.reports} ${s.reports === 1 ? "lab report" : "lab reports"}`
      : undefined,
  ].filter(Boolean)
  return parts.length ? parts.join(", ") : undefined
}

/** The sources of measured values the repository holds, each with its
 * kind and what it holds. */
export function SourceRows(props: { sources: Read<SourceSummary[]> }) {
  const { data, error } = props.sources
  if (!data && !error)
    return (
      <div aria-busy="true" aria-label="Reading your sources">
        <SkeletonRow hasAvatar={false} />
      </div>
    )
  if (!data)
    return (
      <p className="px-4 py-2.5 text-[15px] text-secondary">
        Your sources couldn't be read just now.
      </p>
    )
  if (!data.length)
    return (
      <p className="px-4 py-2.5 text-[15px] text-secondary">
        No sources yet: nothing has written a measurement, a night, a workout or
        a lab result to your Substrate.
      </p>
    )
  return (
    <>
      {[...data]
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((s) => (
          <div key={s.id} className="px-4 py-2.5">
            <div className="flex flex-wrap items-baseline gap-x-3">
              <span className="min-w-0 flex-1 text-[17px] break-words">
                {s.name}
              </span>
              {s.class && (
                <span className="text-[15px] text-secondary">
                  {CLASS_WORDS[s.class] ?? s.class}
                </span>
              )}
            </div>
            {holds(s) && (
              <p className="text-[13px] text-secondary">{holds(s)}</p>
            )}
          </div>
        ))}
    </>
  )
}
