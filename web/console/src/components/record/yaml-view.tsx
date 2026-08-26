/** The manifest as the page: YAML tinted by shiki (lazy-loaded, css-variables
 * theme so it follows the app tokens), a copy affordance on hover (rule 10),
 * the kind's schema docs as Tooltips on the keys it declares — type AND the
 * record-56 one-liner — and references as real links: edge target ids,
 * canonicalId/formerIds, `kind:` references, and the provenance block's
 * manager/actor names all navigate (owner redline, 2026-08-06).
 *
 * ONE render path, tinted or not. The annotations are the SCHEMA's, not the
 * highlighter's, so a line renders its hovers and links whether shiki's chunk
 * has landed, is still in flight, or never arrives — the earlier two-branch
 * shape silently dropped every hover along with the color. */

import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { CheckIcon, CopyIcon } from "lucide-react"

import { TokenSpan } from "@/components/code-block"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useCodeTokens } from "@/lib/code"
import type { CodeToken } from "@/lib/shiki"
import { cn } from "@/lib/utils"
import {
  describableSpan,
  linkableSpan,
  splitAround,
  NO_TARGETS,
  type KeyDoc,
  type KeyDocs,
  type YamlLinkTargets,
} from "@/lib/yaml-annotations"

export type { KeyDoc, KeyDocs, YamlLinkTargets }

/** What a described key says on hover: the one-liner, and under it the
 * declared datatype in the data voice. */
function DocBody({ doc }: { doc: KeyDoc }) {
  return (
    <span className="flex flex-col gap-0.5">
      {doc.description && <span>{doc.description}</span>}
      {doc.type && (
        <span className={cn("data", doc.description && "opacity-70")}>
          {doc.type}
        </span>
      )}
    </span>
  )
}

function LineView({
  tokens,
  line,
  docs,
  targets,
}: {
  /** shiki's tinting for this line; absent until (or unless) it arrives. */
  tokens: CodeToken[] | undefined
  line: string
  docs: KeyDocs
  targets: YamlLinkTargets
}) {
  const hover = describableSpan(line, docs)
  const link = linkableSpan(line, targets)
  // Untinted, the raw line is cut into the same runs the tokens would have
  // produced around the annotated spans — one renderer, two sources of runs.
  const runs: CodeToken[] =
    tokens ??
    splitAround(line, [hover?.text, link?.text]).map((content) => ({
      content,
      italic: false,
    }))

  // Each annotation lands on its FIRST matching run, and never on the same
  // run twice: a line reading `name: name` describes the key, not the value
  // that echoes it.
  const describedAt = hover
    ? runs.findIndex((t) => t.content.trim() === hover.text)
    : -1
  const linkedAt = link
    ? runs.findIndex(
        (t, i) => i !== describedAt && t.content.trim() === link.text
      )
    : -1

  return (
    <span className="block min-h-[1lh]">
      {runs.map((token, i) => {
        if (hover && i === describedAt) {
          return (
            <Tooltip key={i}>
              <TooltipTrigger
                render={
                  <span
                    style={{ color: token.color }}
                    className={cn(
                      "cursor-help underline decoration-dotted decoration-from-font underline-offset-4",
                      token.italic && "italic"
                    )}
                  />
                }
              >
                {token.content}
              </TooltipTrigger>
              <TooltipContent side="right">
                <DocBody doc={hover.doc} />
              </TooltipContent>
            </Tooltip>
          )
        }
        if (link && i === linkedAt) {
          return (
            <Link
              key={i}
              to={link.href}
              style={{ color: token.color }}
              className={cn(
                "underline-offset-4 hover:underline",
                token.italic && "italic"
              )}
            >
              {token.content}
            </Link>
          )
        }
        return <TokenSpan key={i} token={token} />
      })}
    </span>
  )
}

export function YamlView({
  source,
  docs,
  targets = NO_TARGETS,
}: {
  source: string
  docs: KeyDocs
  targets?: YamlLinkTargets
}) {
  const [copied, setCopied] = useState(false)

  const tokens = useCodeTokens(source, "yaml")

  // The lines are the layout — a line count that never depends on the
  // highlighter means the block does not reflow when the tint lands.
  const lines = source.split("\n")

  function copy() {
    void navigator.clipboard.writeText(source).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  return (
    <div className="group relative">
      <Button
        variant="ghost"
        size="sm"
        onClick={copy}
        className="absolute top-2 right-2 h-7 gap-1.5 text-xs text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
      >
        {copied ? (
          <>
            <CheckIcon className="size-3.5" /> Copied
          </>
        ) : (
          <>
            <CopyIcon className="size-3.5" /> Copy
          </>
        )}
      </Button>
      {/* pre-wrap, not overflow-x: a long scalar (an agent's prompt) wraps
          where the reader is, instead of hiding past the right edge. */}
      <pre className="p-4 data text-xs leading-relaxed break-words whitespace-pre-wrap">
        {lines.map((line, i) => (
          <LineView
            key={i}
            tokens={tokens?.[i]}
            line={line}
            docs={docs}
            targets={targets}
          />
        ))}
      </pre>
    </div>
  )
}
