import { displayServerVersion } from "@substrate/client"
import type { Change } from "@substrate/auth"
import { useSubstrate } from "./context"
import { formatDay } from "./status"
import { ProblemBox, type Tone } from "./ui"

function joinWords(words: string[]): string {
  if (words.length < 2) return words.join("")
  return `${words.slice(0, -1).join(", ")} and ${words.at(-1)}`
}

function Changes({ changes }: { changes: Change[] }) {
  if (!changes.length) return null
  return (
    <ul className="mt-2 space-y-1 text-[15px]">
      {changes.map((c) => (
        <li key={`${c.kind}|${c.words}`}>
          <span className="block">{c.words}</span>
          {c.detail && (
            <span className="block text-[13px] break-words text-secondary">
              {c.detail}
            </span>
          )}
        </li>
      ))}
    </ul>
  )
}

/** The one thing about the app's access the owner should act on now, from
 * the last status check: a server update, an approval to review, or access
 * ending soon (the kit's AccessPresenter). Nothing while there is none, and
 * nothing for a check the network stopped, which the status line says. */
export function AccessBanner(props: { className?: string }) {
  const { manifest, session, appStatus, signInAgain } = useSubstrate()
  const issue = appStatus?.issues.find((i) => i.type !== "unreachable")
  if (!issue || issue.type === "accessEnded") return null
  const app = manifest.name
  const host = new URL(session.server).host
  let title: string
  let message: string
  let tone: Tone = "caution"
  let changes: Change[] = []
  let action: string | undefined
  switch (issue.type) {
    case "serverTooOld": {
      const cannot = issue.missing.length
        ? `can't yet ${joinWords(issue.missing)}`
        : issue.blocked.length
          ? `can't yet set up everything ${app} needs`
          : ""
      const runs = issue.running
        ? `${host} runs ${displayServerVersion(issue.running)}`
        : undefined
      message = issue.needed
        ? `${app} needs Substrate ${issue.needed} or later${runs ? `; ${runs}.` : "."}${cannot ? ` It ${cannot}.` : ""}`
        : runs
          ? `${runs}, which ${cannot || `is too old for ${app}`}.`
          : `${host} ${cannot || `is too old for ${app}`}.`
      title = `${host} needs an update`
      message += " Whoever runs it can update it."
      changes = issue.blocked
      break
    }
    case "needsApproval": {
      changes = issue.changes
      const blocked = changes.some((c) => c.block)
      const count =
        changes.length === 1 ? "a change" : `${changes.length} changes`
      if (blocked) {
        title = `Something on ${host} is in the way`
        message = `${app} needs ${count} that can't be approved yet. The approval page on ${host} shows why:`
      } else {
        title = `${app} needs your approval`
        tone = "informative"
        message =
          changes.length === 1
            ? `One change, which you approve on ${host}:`
            : `${changes.length} changes, which you approve on ${host}:`
      }
      action = "Review"
      break
    }
    case "accessEndsSoon":
      title = `Access ends ${formatDay(issue.at)}`
      message = `${app} stops working with ${host} then, until you approve it again.`
      tone = "informative"
      action = "Renew"
      break
  }
  return (
    <div className={props.className}>
      <div className="overflow-hidden rounded-[10px] bg-card">
        <ProblemBox
          title={title}
          message={message}
          tone={tone}
          action={
            action
              ? { title: action, onClick: () => void signInAgain() }
              : undefined
          }
        >
          <Changes changes={changes} />
        </ProblemBox>
      </div>
    </div>
  )
}
