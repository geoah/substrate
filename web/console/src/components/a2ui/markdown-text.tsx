/** A Text's Markdown as elements, never as HTML. A heading starts at `h2`:
 * the host's title is the page's `h1`. Inside a button every block is a
 * span drawn as a block: a button holds phrasing content alone, and a
 * heading there would be a landmark nobody can reach. */

import { Fragment, useMemo } from "react"

import { mdBlocks, type MdBlock, type MdInline } from "@/lib/a2ui/markdown"
import { cn } from "@/lib/utils"

function Runs({ runs }: { runs: MdInline[] }) {
  return runs.map((run, i) => {
    switch (run.type) {
      case "text":
        return <Fragment key={i}>{run.text}</Fragment>
      case "strong":
        return (
          <strong key={i} className="font-semibold">
            <Runs runs={run.runs} />
          </strong>
        )
      case "em":
        return (
          <em key={i}>
            <Runs runs={run.runs} />
          </em>
        )
      case "code":
        return (
          <code
            key={i}
            className="rounded bg-muted px-1 py-px font-mono text-[0.9em]"
          >
            {run.text}
          </code>
        )
    }
  })
}

const HEADINGS = {
  1: { Tag: "h2", className: "text-xl font-semibold tracking-tight" },
  2: { Tag: "h3", className: "text-lg font-semibold" },
  3: { Tag: "h4", className: "text-base font-semibold" },
  4: { Tag: "h5", className: "font-semibold" },
} as const

function Block({ block, inline }: { block: MdBlock; inline: boolean }) {
  switch (block.type) {
    case "heading": {
      const { Tag, className } = HEADINGS[block.level]
      const As = inline ? "span" : Tag
      return (
        <As className={cn("block text-foreground", className)}>
          <Runs runs={block.runs} />
        </As>
      )
    }
    case "paragraph": {
      const As = inline ? "span" : "p"
      return (
        <As className="block">
          {block.lines.map((line, i) => (
            <Fragment key={i}>
              {i > 0 && <br />}
              <Runs runs={line} />
            </Fragment>
          ))}
        </As>
      )
    }
    case "list": {
      if (inline)
        return (
          <span className="block space-y-0.5">
            {block.items.map((item, i) => (
              <span key={i} className="block">
                {block.ordered ? `${i + 1}. ` : "• "}
                <Runs runs={item} />
              </span>
            ))}
          </span>
        )
      const Tag = block.ordered ? "ol" : "ul"
      return (
        <Tag
          className={cn(
            "space-y-0.5 pl-5",
            block.ordered ? "list-decimal" : "list-disc"
          )}
        >
          {block.items.map((item, i) => (
            <li key={i}>
              <Runs runs={item} />
            </li>
          ))}
        </Tag>
      )
    }
    case "code": {
      const As = inline ? "span" : "pre"
      return (
        <As className="block overflow-x-auto rounded-md bg-muted p-2.5 font-mono text-xs whitespace-pre">
          {block.text}
        </As>
      )
    }
  }
}

export function MarkdownText({
  text,
  inline = false,
}: {
  text: string
  /** Drawn inside a button: spans alone. */
  inline?: boolean
}) {
  const blocks = useMemo(() => mdBlocks(text), [text])
  return blocks.map((block, i) => (
    <Block key={i} block={block} inline={inline} />
  ))
}
