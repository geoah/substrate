/** A record named inside prose: an inline atom that reads as the record's
 * mark (glyph and live title) and is stored as a plain Markdown link whose
 * target is `substrate://` and the record path a reference property stores
 * (decision record 0137). Any Markdown reader shows the link text; the
 * console parses the scheme back into the mark. The link is prose, not a
 * reference property: the substrate does not index it and the referent's
 * history does not see it. */

import { Node, mergeAttributes, type Editor } from "@tiptap/core"
import { ReactNodeViewRenderer } from "@tiptap/react"

import { RecordLinkView } from "./record-link-view"
import { recordPath, splitRecordPath } from "@/lib/record-path"

export const RECORD_SCHEME = "substrate://"

export interface RecordLinkAttrs {
  kind: string
  id: string
  /** The title when the link was written: the link text other readers see. */
  title: string
}

/** `[text](substrate://<kind>/<id>)`, the text with `\`, `[` and `]` escaped. */
const LINK = /^\[((?:\\.|[^\\\]])*)\]\(substrate:\/\/([^)\s]+)\)/
const LINK_START = /\[(?:\\.|[^\\\]])*\]\(substrate:\/\//

/** The Markdown a record link is stored as. */
export function recordLinkMarkdown({ kind, id, title }: RecordLinkAttrs) {
  const text = (title || id).replace(/[\\[\]]/g, (c) => `\\${c}`)
  return `[${text}](${RECORD_SCHEME}${recordPath(kind, id)})`
}

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    recordLink: {
      /** Insert a record link, and a space after it, at the selection. */
      insertRecordLink: (attrs: RecordLinkAttrs) => ReturnType
    }
  }
  interface Storage {
    recordLink: {
      /** The kind the next record picker is narrowed to. */
      kind?: string
    }
  }
}

export const RecordLink = Node.create({
  name: "recordLink",
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,

  addAttributes() {
    // Carried by the node, never written as HTML attributes: an `id` on
    // the anchor would be a DOM id.
    return {
      kind: { default: "", rendered: false },
      id: { default: "", rendered: false },
      title: { default: "", rendered: false },
    }
  },

  parseHTML() {
    return [
      {
        tag: "a[data-record-link]",
        // Before the link mark's `a[href]`, which would read it as a link.
        priority: 100,
        getAttrs: (el) => {
          const hit = splitRecordPath(el.getAttribute("data-record-link") ?? "")
          return hit ? { ...hit, title: el.textContent ?? "" } : false
        },
      },
    ]
  },

  // What a copy carries: the record's console page as an absolute URL, so
  // a paste anywhere else is a working link, and the record path, so a paste
  // back into an editor is the record link again.
  renderHTML({ node, HTMLAttributes }) {
    const { kind, id, title } = node.attrs as RecordLinkAttrs
    const path = recordPath(kind, id)
    return [
      "a",
      mergeAttributes(HTMLAttributes, {
        "data-record-link": path,
        href: `${globalThis.location?.origin ?? ""}/data/${path}`,
      }),
      title || id,
    ]
  },

  renderText({ node }) {
    const { id, title } = node.attrs as RecordLinkAttrs
    return title || id
  },

  markdownTokenizer: {
    name: "recordLink",
    level: "inline",
    start: (src) => src.search(LINK_START),
    tokenize(src) {
      const match = LINK.exec(src)
      const hit = match && splitRecordPath(match[2])
      if (!match || !hit) return undefined
      return {
        type: "recordLink",
        raw: match[0],
        text: match[1].replace(/\\(.)/g, "$1"),
        kind: hit.kind,
        id: hit.id,
      }
    },
  },

  parseMarkdown: (token) => ({
    type: "recordLink",
    attrs: {
      kind: token.kind as string,
      id: token.id as string,
      title: token.text ?? "",
    },
  }),

  renderMarkdown: (node) =>
    recordLinkMarkdown((node.attrs ?? {}) as RecordLinkAttrs),

  addStorage() {
    return { kind: undefined as string | undefined }
  },

  addCommands() {
    return {
      insertRecordLink:
        (attrs) =>
        ({ commands }) =>
          commands.insertContent([
            { type: this.name, attrs },
            { type: "text", text: " " },
          ]),
    }
  },

  addNodeView() {
    return ReactNodeViewRenderer(RecordLinkView, { as: "span" })
  },
})

/** Open the record picker at the cursor, narrowed to one kind or not. The
 * picker is the `@` suggestion, so opening it is typing its trigger. */
export function openRecordPicker(editor: Editor, kind?: string) {
  editor.storage.recordLink.kind = kind
  const { $from } = editor.state.selection
  const before = $from.parent.textBetween(
    Math.max(0, $from.parentOffset - 1),
    $from.parentOffset
  )
  // The trigger needs a space or the line's start before it.
  editor
    .chain()
    .focus()
    .insertContent(before && !/\s/.test(before) ? " @" : "@")
    .run()
}
