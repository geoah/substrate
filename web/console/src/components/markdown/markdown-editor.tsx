/** A `markdown` property, read and written as a document. The stored value
 * stays Markdown (CommonMark with GFM tables and task lists); the editor
 * parses it on the way in and serializes it on the way out, so a record
 * written here reads the same through the API and the CLI. Markdown
 * shortcuts work as typed (`# `, `- `, `> `, `**bold**`), `/` opens the
 * block menu, and `@` or ⌘K opens the record picker. Read-only, the same
 * editor renders the text, so reading and editing never lay out differently. */

import { useEffect } from "react"
import type { Editor } from "@tiptap/core"
import { EditorContent, useEditor } from "@tiptap/react"

import { markdownExtensions, storedMarkdown } from "./extensions"
import { cn } from "@/lib/utils"

export function MarkdownEditor({
  value,
  label,
  editable,
  placeholder = "Write, type / for blocks or @ to link a record…",
  autoFocus = false,
  disabled = false,
  onChange,
  onReady,
  className,
}: {
  value: string
  label: string
  editable: boolean
  placeholder?: string
  autoFocus?: boolean
  disabled?: boolean
  onChange?: (markdown: string) => void
  /** The editor, once made: what a caller that drives it directly holds. */
  onReady?: (editor: Editor) => void
  className?: string
}) {
  const editor = useEditor({
    extensions: markdownExtensions(placeholder),
    content: value,
    contentType: "markdown",
    editable,
    immediatelyRender: true,
    shouldRerenderOnTransaction: false,
    onCreate: ({ editor }) => onReady?.(editor),
    onUpdate: ({ editor }) => onChange?.(storedMarkdown(editor)),
  })

  const live = editable && !disabled
  // StrictMode mounts twice and the first editor is destroyed under it.
  useEffect(() => {
    if (editor.isDestroyed) return
    editor.setEditable(live, false)
    editor.setOptions({
      editorProps: {
        // Tiptap puts `role="textbox"` under these attributes whether or not
        // the editor is editable, and ProseMirror cannot unset one, so the
        // read-only view names its own role or reads as an input.
        attributes: live
          ? {
              role: "textbox",
              "aria-multiline": "true",
              "aria-label": label,
              class: "outline-none",
            }
          : { role: "document", "aria-label": label, class: "outline-none" },
      },
    })
    if (live && autoFocus) editor.commands.focus("end")
  }, [editor, live, label, autoFocus])

  // A value that changes while nobody is editing is somebody else's write.
  useEffect(() => {
    if (editable || editor.isDestroyed) return
    if (storedMarkdown(editor) === value) return
    editor.commands.setContent(value, {
      contentType: "markdown",
      emitUpdate: false,
    })
  }, [editor, editable, value])

  return (
    <EditorContent
      editor={editor}
      data-slot="markdown"
      className={cn("markdown", className)}
    />
  )
}
