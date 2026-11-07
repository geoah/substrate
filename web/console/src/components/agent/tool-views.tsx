/** The views of the result shapes the console knows (lib/tool-views.ts):
 * what a tool found, drawn the way a reader expects it, under the call's
 * line and without opening it. A search is its results as numbered source
 * pills, a page read is the page as one, each opening the page in a new
 * tab. A new shape is a case here beside its reader. */

import { GlobeIcon } from "lucide-react"

import { ExternalLink } from "@/components/external-link"
import {
  webHost,
  type ToolResult,
  type WebHit,
  type WebPage,
} from "@/lib/tool-views"

/** One web page as a pill: the site, the page's title, the new-tab mark. */
export function SourcePill({
  url,
  title,
  index,
  description,
}: {
  url: string
  title?: string
  /** The number a reply cites it by, when it has one. */
  index?: number
  /** Shown on hover, such as a search hit's snippet. */
  description?: string
}) {
  const host = webHost(url)
  return (
    <ExternalLink
      href={url}
      nofollow
      title={description ? `${title ?? host}\n\n${description}` : url}
      className="inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-full border bg-background px-2.5 py-0.5 text-[12px] text-muted-foreground no-underline hover:bg-hover hover:text-foreground"
      iconClassName="ml-0"
    >
      {index !== undefined ? (
        <span className="shrink-0 font-medium text-faint tabular-nums">
          {index}
        </span>
      ) : (
        <GlobeIcon aria-hidden className="size-3 shrink-0 text-faint" />
      )}
      <span className="shrink-0 text-faint">{host}</span>
      {title && title !== host && (
        <span className="max-w-[22rem] min-w-0 truncate text-foreground">
          {title}
        </span>
      )}
    </ExternalLink>
  )
}

function WebHits({ hits }: { hits: WebHit[] }) {
  if (hits.length === 0) {
    return <span className="text-[12.5px] text-faint">No results.</span>
  }
  return (
    <ol aria-label="Results" className="flex flex-wrap gap-1.5">
      {hits.map((hit, i) => (
        <li key={`${hit.url}:${i}`} className="max-w-full min-w-0">
          <SourcePill
            url={hit.url}
            title={hit.title}
            index={i + 1}
            description={hit.snippet}
          />
        </li>
      ))}
    </ol>
  )
}

function WebPageView({ page }: { page: WebPage }) {
  return <SourcePill url={page.url} title={page.title} />
}

/** A result of a known shape, drawn. */
export function ToolResultView({ result }: { result: ToolResult }) {
  switch (result.type) {
    case "web-hits":
      return <WebHits hits={result.hits} />
    case "web-page":
      return <WebPageView page={result.page} />
  }
}
