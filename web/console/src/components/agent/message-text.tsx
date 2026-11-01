/** An agent's reply, rendered: paragraphs, lists, headings, code and links
 * from the markdown subset `chatBlocks` reads, and every record the reply
 * names, by path or by record link, as the record itself — unless the
 * engine found no such record when the run settled (`mentions`), in which
 * case the link's text stands alone. Model-authored text becomes elements,
 * never HTML. */

import { Fragment } from "react"

import { RecordRef } from "@/components/identity/record-ref"
import type { AgentMention } from "@/lib/api/agents"
import { chatBlocks, type Inline } from "@/lib/chat-text"

/** The `kind/id` paths of the mentions the engine found no record for. */
function missingPaths(mentions: AgentMention[] | undefined): Set<string> {
  return new Set(
    (mentions ?? []).filter((m) => m.missing).map((m) => `${m.kind}/${m.id}`)
  )
}

function Runs({ runs, missing }: { runs: Inline[]; missing: Set<string> }) {
  return (
    <>
      {runs.map((run, i) => {
        switch (run.type) {
          case "strong":
            return (
              <strong key={i} className="font-semibold">
                <Runs runs={run.runs} missing={missing} />
              </strong>
            )
          case "code":
            return (
              <code
                key={i}
                className="rounded-[4px] bg-hover px-1 py-px font-mono text-[0.9em]"
              >
                {run.text}
              </code>
            )
          case "record":
            if (missing.has(`${run.kind}/${run.id}`)) {
              return <Fragment key={i}>{run.text}</Fragment>
            }
            return <RecordRef key={i} kind={run.kind} id={run.id} />
          case "link":
            // A page outside the console: a new tab in a browser, the system
            // browser in the Apple shell. No referrer and no opener, because
            // the address is the model's choice.
            return (
              <a
                key={i}
                href={run.href}
                title={run.href}
                target="_blank"
                rel="noopener noreferrer nofollow"
                className="text-primary-text underline underline-offset-2"
              >
                {run.text}
              </a>
            )
          default:
            return <Fragment key={i}>{run.text}</Fragment>
        }
      })}
    </>
  )
}

export function MessageText({
  text,
  mentions,
}: {
  text: string
  mentions?: AgentMention[]
}) {
  const blocks = chatBlocks(text)
  const missing = missingPaths(mentions)
  return (
    <div className="flex flex-col gap-2.5 [overflow-wrap:anywhere]">
      {blocks.map((block, i) => {
        switch (block.type) {
          case "heading":
            return (
              <p key={i} className="font-semibold">
                <Runs runs={block.runs} missing={missing} />
              </p>
            )
          case "code":
            return (
              <pre
                key={i}
                className="overflow-x-auto rounded-lg border bg-panel px-3 py-2 font-mono text-[12.5px] leading-relaxed"
              >
                {block.text}
              </pre>
            )
          case "list": {
            const List = block.ordered ? "ol" : "ul"
            return (
              <List
                key={i}
                className={
                  block.ordered
                    ? "flex list-decimal flex-col gap-1.5 pl-5"
                    : "flex flex-col gap-1.5"
                }
              >
                {block.items.map((item, j) => (
                  <li
                    key={j}
                    className={
                      block.ordered
                        ? undefined
                        : "relative pl-3.5 before:absolute before:top-[0.7em] before:left-0.5 before:size-[5px] before:rounded-full before:bg-faint-deco"
                    }
                  >
                    <Runs runs={item} missing={missing} />
                  </li>
                ))}
              </List>
            )
          }
          default:
            return (
              <p key={i}>
                {block.lines.map((line, j) => (
                  <Fragment key={j}>
                    {j > 0 && <br />}
                    <Runs runs={line} missing={missing} />
                  </Fragment>
                ))}
              </p>
            )
        }
      })}
    </div>
  )
}
