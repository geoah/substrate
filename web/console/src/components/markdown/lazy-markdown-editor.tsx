/** The Markdown editor, loaded with the first `markdown` property a page
 * shows and never with the rest of the console (the YAML editor's
 * discipline). Until it arrives the value reads as plain paragraphs. Every
 * surface that shows or edits a `markdown` property goes through this. */

import { Suspense, lazy, type ComponentProps } from "react"

import type { MarkdownEditor as Editor } from "./markdown-editor"

const MarkdownEditor = lazy(() =>
  import("./markdown-editor").then((m) => ({ default: m.MarkdownEditor }))
)

export function PlainMarkdown({ text }: { text: string }) {
  return text.split(/\n{2,}/).map((p, i) => (
    <p key={i} className="mb-[0.8em] whitespace-pre-wrap last:mb-0">
      {p}
    </p>
  ))
}

export function LazyMarkdownEditor(props: ComponentProps<typeof Editor>) {
  return (
    <Suspense fallback={<PlainMarkdown text={props.value} />}>
      <MarkdownEditor {...props} />
    </Suspense>
  )
}
