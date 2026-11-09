import { Fragment, type ReactNode } from "react"

/** A record's `markdown` (a letter's words, a consultation, a message):
 * drawn as React elements from a small subset (paragraphs, headings, lists
 * with their check marks, quotes, code, emphasis, links), never as HTML,
 * so nothing a provider wrote runs or styles the page. A link opens only
 * an http, https or mailto address. Tasks draws its notes the same way. */
export function Markdown({ text }: { text: string }) {
  return <div className="space-y-2 break-words">{blocks(text)}</div>
}

type Block =
  | { kind: "p"; lines: string[] }
  | { kind: "h"; level: number; text: string }
  | { kind: "list"; ordered: boolean; items: string[] }
  | { kind: "quote"; lines: string[] }
  | { kind: "code"; lines: string[] }

const bullet = /^\s*[-*+]\s+(.*)$/
const numbered = /^\s*\d+[.)]\s+(.*)$/

function parse(text: string): Block[] {
  const out: Block[] = []
  const lines = text.replace(/\r\n?/g, "\n").split("\n")
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (/^\s*```/.test(line)) {
      const code: string[] = []
      while (++i < lines.length && !/^\s*```/.test(lines[i]))
        code.push(lines[i])
      out.push({ kind: "code", lines: code })
      continue
    }
    if (!line.trim()) continue
    const heading = /^(#{1,6})\s+(.*)$/.exec(line)
    if (heading) {
      out.push({ kind: "h", level: heading[1].length, text: heading[2] })
      continue
    }
    const last = out.at(-1)
    const item = bullet.exec(line) ?? numbered.exec(line)
    if (item) {
      const ordered = !bullet.test(line)
      if (last?.kind === "list" && last.ordered === ordered)
        last.items.push(item[1])
      else out.push({ kind: "list", ordered, items: [item[1]] })
      continue
    }
    const quote = /^\s*>\s?(.*)$/.exec(line)
    if (quote) {
      if (last?.kind === "quote") last.lines.push(quote[1])
      else out.push({ kind: "quote", lines: [quote[1]] })
      continue
    }
    // A line under a paragraph or a list item continues it.
    const previous = lines[i - 1]
    if (last?.kind === "p" && previous?.trim()) last.lines.push(line)
    else if (last?.kind === "list" && previous?.trim() && /^\s+/.test(line))
      last.items[last.items.length - 1] += ` ${line.trim()}`
    else out.push({ kind: "p", lines: [line] })
  }
  return out
}

function blocks(text: string): ReactNode[] {
  return parse(text).map((b, i) => {
    switch (b.kind) {
      case "h":
        return (
          <p
            key={i}
            className={
              b.level <= 2 ? "text-[19px] font-semibold" : "font-semibold"
            }
          >
            {inline(b.text)}
          </p>
        )
      case "list": {
        const Tag = b.ordered ? "ol" : "ul"
        return (
          <Tag
            key={i}
            className={b.ordered ? "list-decimal pl-6" : "list-disc pl-6"}
          >
            {b.items.map((item, j) => {
              const check = /^\[( |x|X)\]\s+(.*)$/.exec(item)
              return check ? (
                <li key={j} className="-ml-5 list-none">
                  <span aria-hidden className="mr-1.5 inline-block w-3.5">
                    {check[1] === " " ? "☐" : "☑"}
                  </span>
                  <span className="sr-only">
                    {check[1] === " " ? "Not done: " : "Done: "}
                  </span>
                  {inline(check[2])}
                </li>
              ) : (
                <li key={j}>{inline(item)}</li>
              )
            })}
          </Tag>
        )
      }
      case "quote":
        return (
          <blockquote
            key={i}
            className="border-l-[3px] border-separator pl-3 text-secondary"
          >
            {lines(b.lines)}
          </blockquote>
        )
      case "code":
        return (
          <pre
            key={i}
            className="overflow-x-auto rounded-lg bg-fill px-3 py-2 font-mono text-[14px] whitespace-pre"
          >
            {b.lines.join("\n")}
          </pre>
        )
      case "p":
        return <p key={i}>{lines(b.lines)}</p>
    }
  })
}

function lines(ls: string[]): ReactNode[] {
  return ls.map((l, i) => (
    <Fragment key={i}>
      {i > 0 && <br />}
      {inline(l)}
    </Fragment>
  ))
}

/** The address a link may open, or undefined. */
export function safeURL(href: string): string | undefined {
  try {
    const url = new URL(href)
    return ["http:", "https:", "mailto:"].includes(url.protocol)
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

const inlinePattern =
  /`([^`]+)`|\[([^\]]+)\]\(((?:[^()\s]|\([^()\s]*\))+)\)|\*\*(.+?)\*\*|__(.+?)__|~~(.+?)~~|(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?![\w*])|(?<![\w_])_(?!\s)(.+?)(?<!\s)_(?![\w_])|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])/

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = []
  let rest = text
  let key = 0
  while (rest) {
    const m = inlinePattern.exec(rest)
    if (!m) {
      out.push(rest)
      break
    }
    if (m.index > 0) out.push(rest.slice(0, m.index))
    const [whole, code, label, href, strong1, strong2, struck, em1, em2, bare] =
      m
    const k = key++
    if (code !== undefined)
      out.push(
        <code key={k} className="rounded bg-fill px-1 font-mono text-[0.9em]">
          {code}
        </code>
      )
    else if (label !== undefined) out.push(link(k, label, href))
    else if (strong1 ?? strong2)
      out.push(<strong key={k}>{inline(strong1 ?? strong2)}</strong>)
    else if (struck !== undefined) out.push(<s key={k}>{inline(struck)}</s>)
    else if (em1 ?? em2) out.push(<em key={k}>{inline(em1 ?? em2)}</em>)
    else if (bare !== undefined) out.push(link(k, bare, bare))
    rest = rest.slice(m.index + whole.length)
  }
  return out
}

function link(key: number, label: string, href: string): ReactNode {
  const url = safeURL(href)
  if (!url) return <Fragment key={key}>{label}</Fragment>
  return (
    <a
      key={key}
      href={url}
      target="_blank"
      rel="noreferrer noopener"
      className="text-accent-text underline decoration-accent-text/40 underline-offset-2"
    >
      {label}
    </a>
  )
}
