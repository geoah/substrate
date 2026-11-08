import {
  scopeWords,
  unrevokedSentences,
  unrevokedTitle,
  type PackageStanding,
} from "@substrate/auth"
import { displayServerVersion } from "@substrate/client"
import type { OutboxEntry } from "@substrate/local"
import { useEffect, useState, type ReactNode } from "react"
import { useSubstrate } from "./context"
import { useLinkStatus, useLive, useOutbox } from "./hooks"
import { formatMoment } from "./status"
import { useToast } from "./toast"
import {
  ActionRow,
  Confirm,
  NoteRow,
  ProblemBox,
  Row,
  Section,
  VerbatimRow,
} from "./ui"

/** A kind by what a person calls it, from its reference's last segment
 * where the app names it nothing: "Task". */
function kindWord(kind: string, labels?: Record<string, string>): string {
  const named = labels?.[kind]
  if (named) return named
  const last = kind.split("/").at(-1) ?? kind
  return last.charAt(0).toUpperCase() + last.slice(1)
}

function packageValue(p: PackageStanding, checking: boolean): string {
  if (p.held === undefined) return checking ? "Checking" : "Not checked yet"
  if (p.held === "missing") return "Not installed"
  if (p.held < p.needs)
    return `Version ${p.held}, waiting for the update to version ${p.needs}`
  return `Version ${p.held}`
}

/** Settings' Status: where the app stands with its repository in one
 * sentence, why where there is a reason, each package the app needs, and
 * what the owner can do about it (the kit's StatusPresenter). */
export function StatusSection() {
  const { session, outbox, appStatus, checking, signInAgain } = useSubstrate()
  const summary = useOutbox()
  const { status, sentence, tone } = useLinkStatus()
  const host = new URL(session.server).host
  const actions: { title: string; run: () => void }[] = []
  if (summary.waiting > 0 || summary.flushing)
    actions.push({ title: "Send Now", run: () => void outbox.sendNow() })
  if (status.type === "needsApproval")
    actions.push({ title: "Review Access…", run: () => void signInAgain() })
  if (status.type === "signedOut")
    actions.push({ title: "Sign In Again…", run: () => void signInAgain() })
  if (status.type === "accessEndsSoon")
    actions.push({ title: "Renew Access…", run: () => void signInAgain() })
  const problem =
    status.type === "notKept"
      ? { title: "Not kept in this browser", message: status.reason }
      : status.type === "refused" && !summary.parked.length && status.reason
        ? { title: "Refused by your substrate", message: status.reason }
        : undefined
  const footer = [
    `Changes are written to ${host} as you make them, and wait in this browser while it cannot be reached.`,
    summary.persistent ? "" : (summary.storageProblem ?? ""),
  ]
    .filter(Boolean)
    .join(" ")
  return (
    <Section header="Your Substrate" footer={footer}>
      <NoteRow tone={tone}>{sentence}</NoteRow>
      {problem && (
        <ProblemBox
          title={problem.title}
          message={problem.message}
          tone={status.type === "refused" ? "caution" : "critical"}
        />
      )}
      {(appStatus?.packages ?? []).map((p) => (
        <Row
          key={p.id}
          label={
            <span className="block">
              {p.name} package
              <span className="block font-mono text-[13px] break-all text-secondary">
                {p.id}
              </span>
            </span>
          }
          value={packageValue(p, checking)}
        />
      ))}
      {actions.map((a) => (
        <ActionRow
          key={a.title}
          onClick={a.run}
          disabled={summary.flushing && a.title === "Send Now"}
        >
          {a.title}
        </ActionRow>
      ))}
    </Section>
  )
}

function refusal(entry: OutboxEntry): string {
  if (entry.method === "DELETE") return "Delete refused"
  if (entry.method === "PATCH") return "Change refused"
  return "Write refused"
}

/** The writes the server refused, each with what it was of, the server's
 * words, and the owner's two ways out: Retry and Discard, or for an edit
 * that met a newer version, Keep My Edit and Use Theirs. Nothing shows
 * while none waits. */
export function RefusedWritesSection(props: {
  /** What a person calls each kind, by reference: "Task". */
  kindLabels?: Record<string, string>
}) {
  const { outbox } = useSubstrate()
  const summary = useOutbox()
  const toast = useToast()
  if (!summary.parked.length) return null
  const run = (p: Promise<void>) =>
    void p.catch((e: unknown) =>
      toast(e instanceof Error ? e.message : String(e), "error")
    )
  const conflicts = summary.parked.some((e) => e.conflict)
  return (
    <Section
      header="Refused by Your Substrate"
      footer={
        "Retry sends the change again. Discard gives up the change and the changes waiting behind it; what this browser holds stays." +
        (conflicts
          ? " Keep My Edit sends your edit over the other change; Use Theirs keeps what your substrate holds."
          : "")
      }
    >
      {summary.parked.map((e) => (
        <div
          key={e.id}
          role="group"
          aria-label={kindWord(e.kind, props.kindLabels)}
        >
          <div className="px-4 py-2.5">
            <div className="text-[17px]">
              {kindWord(e.kind, props.kindLabels)}
            </div>
            {e.lastError && (
              <div className="text-[15px] break-words text-secondary">
                {e.lastError}
              </div>
            )}
          </div>
          <NoteRow tone="caution">
            {e.conflict ? "Edit not sent" : refusal(e)}
          </NoteRow>
          <VerbatimRow label="Kind" value={e.kind} />
          <VerbatimRow label="Record ID" value={e.recordID} />
          {e.conflict ? (
            <>
              <ActionRow onClick={() => run(outbox.resolve(e.id, true))}>
                Keep My Edit
              </ActionRow>
              <ActionRow
                destructive
                onClick={() => run(outbox.resolve(e.id, false))}
              >
                Use Theirs
              </ActionRow>
            </>
          ) : (
            <>
              <ActionRow onClick={() => run(outbox.retry(e.id))}>
                Retry
              </ActionRow>
              <ActionRow destructive onClick={() => run(outbox.discard(e.id))}>
                Discard
              </ActionRow>
            </>
          )}
        </div>
      ))}
    </Section>
  )
}

/** The repository the app is signed in to: its server, name and version,
 * the tokens this browser could not revoke, and Sign Out, which asks first
 * (the kit's RepositoryPresenter). */
export function RepositorySection(props: {
  /** What else signing out gives up, in the app's words, after the kit's. */
  signOutMessage?: string
}) {
  const { session, manifest, appStatus, unrevoked, signOut } = useSubstrate()
  const summary = useOutbox()
  const [asking, setAsking] = useState(false)
  const host = new URL(session.server).host
  const waiting = summary.waiting
  const message = [
    `${manifest.name}'s access on ${host} is revoked, and what this browser keeps for ${session.repository} is cleared.`,
    waiting
      ? `The ${waiting === 1 ? "change" : `${waiting} changes`} not sent yet ${waiting === 1 ? "is" : "are"} given up.`
      : "",
    props.signOutMessage ?? "",
  ]
    .filter(Boolean)
    .join(" ")
  return (
    <>
      <Section
        header="Repository"
        footer={`Sign Out: signs out in this browser and revokes its access on ${host}.`}
      >
        <Row label="Server" value={host} />
        <Row
          label="Repository"
          value={session.repository}
          valueClassName="break-all"
        />
        {appStatus?.serverVersion && (
          <Row
            label="Server version"
            value={capitalize(displayServerVersion(appStatus.serverVersion))}
          />
        )}
        {unrevokedSentences(manifest.name, unrevoked).map((n) => (
          <ProblemBox
            key={n}
            tone="caution"
            title={unrevokedTitle}
            message={n}
          />
        ))}
        <ActionRow destructive onClick={() => setAsking(true)}>
          Sign Out
        </ActionRow>
      </Section>
      <Confirm
        open={asking}
        title={`Sign out of ${session.repository}?`}
        message={message}
        confirm="Sign Out"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false)
          void signOut()
        }}
      />
    </>
  )
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

/** The access the owner approved: one row per scope, in words and as the
 * token carries it, until when, and Change Access, which opens the
 * approval page again. */
export function ApprovedAccessSection() {
  const { session, signInAgain } = useSubstrate()
  return (
    <Section header="Approved access">
      {session.scopes.map((s) => (
        <div key={s} className="flex items-start gap-3 px-4 py-2.5">
          <span
            aria-hidden
            className="mt-[7px] size-2.5 shrink-0 rounded-full bg-ok"
          />
          <span className="min-w-0">
            <span className="block text-[17px]">{scopeWords(s)}</span>
            <span className="block font-mono text-[13px] break-all text-secondary">
              {s}
            </span>
          </span>
        </div>
      ))}
      <Row
        label="Until"
        value={
          session.expiresAt ? formatMoment(session.expiresAt) : "You revoke it"
        }
      />
      <ActionRow onClick={() => void signInAgain()}>Change Access…</ActionRow>
    </Section>
  )
}

/** A size as a person reads it: "0 bytes", "12 KB", "1.5 MB", decimal
 * units as the systems' own settings use. */
export function byteWords(bytes: number): string {
  if (bytes < 1000) return bytes === 1 ? "1 byte" : `${bytes} bytes`
  const units = ["KB", "MB", "GB", "TB"]
  let value = bytes / 1000
  let unit = 0
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000
    unit++
  }
  const rounded = Math.round(value * 10) / 10
  const text =
    rounded >= 100 || rounded === Math.round(rounded)
      ? String(Math.round(rounded))
      : rounded.toFixed(1)
  return `${text} ${units[unit]}`
}

/** What this browser keeps for the repository, and a clear that never
 * drops unsent writes: only the saved records clear, and they are read
 * again. */
export function StorageSection() {
  const { storage, cache, linked } = useSubstrate()
  const summary = useOutbox()
  const live = useLive()
  const [usage, setUsage] = useState<{ outbox: number; records: number }>()
  const [asking, setAsking] = useState(false)
  useEffect(() => {
    let current = true
    void storage
      .usage()
      .then((u) => current && setUsage(u))
      .catch(() => {})
    return () => {
      current = false
    }
  }, [storage, summary.revision, live.revision])
  const footer = [
    "Saved copies are read again from your substrate when needed. Changes waiting to be sent are never cleared.",
    storage.persistent ? "" : (storage.problem ?? ""),
  ]
    .filter(Boolean)
    .join(" ")
  return (
    <>
      <Section header="Storage" footer={footer}>
        <Row
          label="Waiting to be sent"
          value={usage ? byteWords(usage.outbox) : "…"}
        />
        <Row
          label="Saved records"
          value={usage ? byteWords(usage.records) : "…"}
        />
        <Row
          label="Total"
          value={usage ? byteWords(usage.outbox + usage.records) : "…"}
        />
        <ActionRow
          destructive
          disabled={!usage?.records}
          onClick={() => setAsking(true)}
        >
          Clear Saved Copies
        </ActionRow>
      </Section>
      <Confirm
        open={asking}
        title="Clear saved copies?"
        message="They are read again from your substrate. Changes waiting to be sent stay."
        confirm="Clear"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false)
          void cache.clear().then(() => linked.reload())
        }}
      />
    </>
  )
}

/** The build: its version, and any rows of the app's own below it. */
export function AboutSection(props: { children?: ReactNode }) {
  const { manifest } = useSubstrate()
  return (
    <Section header="About">
      <Row label="Version" value={manifest.softwareVersion} />
      {props.children}
    </Section>
  )
}

/** Every section the kit gives an app's Settings, in the same order in
 * every app, after the app's own: Status, the writes refused, the
 * repository and its approved access, storage, and about. */
export function SubstrateSettings(props: {
  kindLabels?: Record<string, string>
  signOutMessage?: string
  about?: ReactNode
}) {
  return (
    <>
      <StatusSection />
      <RefusedWritesSection kindLabels={props.kindLabels} />
      <RepositorySection signOutMessage={props.signOutMessage} />
      <ApprovedAccessSection />
      <StorageSection />
      <AboutSection>{props.about}</AboutSection>
    </>
  )
}
