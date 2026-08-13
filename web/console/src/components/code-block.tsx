/** THE tinted code surface: one token renderer, shared by every place the
 * console shows source — the manifest's YAML (`record/yaml-view.tsx`, which
 * wraps the same tokens in its own annotation layer) and the JSON of a tool
 * call's request and response.
 *
 * ONE render path, tinted or not. The tint is an enhancement that arrives late
 * or never; the text and its line count are laid out identically either way, so
 * nothing reflows when shiki's chunk lands. */

import { useCodeTokens } from "@/lib/code"
import type { CodeLang, CodeToken } from "@/lib/shiki"
import { cn } from "@/lib/utils"

export function TokenSpan({ token }: { token: CodeToken }) {
  return (
    <span
      style={{ color: token.color }}
      className={cn(token.italic && "italic")}
    >
      {token.content}
    </span>
  )
}

export function CodeBlock({
  source,
  lang,
  className,
}: {
  source: string
  lang: CodeLang
  className?: string
}) {
  const tokens = useCodeTokens(source, lang)
  const lines = source.split("\n")
  return (
    <pre
      className={cn(
        "overflow-x-auto rounded-sm bg-background/60 p-2 data text-xs leading-relaxed",
        className
      )}
    >
      {lines.map((line, i) => {
        const runs = tokens?.[i]
        return (
          <span key={i} className="block min-h-[1lh]">
            {runs
              ? runs.map((token, j) => <TokenSpan key={j} token={token} />)
              : line}
          </span>
        )
      })}
    </pre>
  )
}
