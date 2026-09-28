/** The editor's extensions (markdown-editor.tsx), kept apart from the
 * component so a headless editor in a test is built from the same list. */

import { Extension, type Editor } from "@tiptap/core"
import { TableKit } from "@tiptap/extension-table"
import { TaskItem, TaskList } from "@tiptap/extension-list"
import { Placeholder } from "@tiptap/extensions"
import { Markdown } from "@tiptap/markdown"
import { PluginKey } from "@tiptap/pm/state"
import { ReactRenderer } from "@tiptap/react"
import StarterKit from "@tiptap/starter-kit"
import Suggestion from "@tiptap/suggestion"
import type { ComponentType } from "react"

import { RecordMenu, SlashMenu, type MenuProps } from "./menus"
import { openRecordPicker, RecordLink } from "./record-link"
import type { MenuKeys } from "./suggestion-menu"

/** A menu at the cursor, opened by typing `char`. The menu reads its own
 * rows from the query and applies its own pick, so the plugin's items and
 * command are unused. */
function menuAt(name: string, char: string, Menu: ComponentType<MenuProps>) {
  return Extension.create({
    name,
    addProseMirrorPlugins() {
      return [
        Suggestion({
          editor: this.editor,
          pluginKey: new PluginKey(name),
          char,
          allow: ({ editor }) =>
            editor.isEditable && !editor.isActive("codeBlock"),
          items: () => [],
          render: () => {
            let renderer: ReactRenderer | undefined
            let unmount: (() => void) | undefined
            const keysRef: MenuKeys = { current: null }
            return {
              onStart: (props) => {
                renderer = new ReactRenderer(Menu, {
                  editor: props.editor,
                  props: { ...props, keysRef },
                })
                unmount = props.mount(renderer.element)
              },
              onUpdate: (props) => renderer?.updateProps({ ...props, keysRef }),
              onKeyDown: ({ event }) => keysRef.current?.(event) ?? false,
              onExit: (props) => {
                unmount?.()
                renderer?.destroy()
                if (char === "@")
                  props.editor.storage.recordLink.kind = undefined
              },
            }
          },
        }),
      ]
    },
  })
}

const SlashCommands = menuAt("slashCommands", "/", SlashMenu)
const RecordMentions = menuAt("recordMentions", "@", RecordMenu)

const RecordShortcut = Extension.create({
  name: "recordShortcut",
  addKeyboardShortcuts() {
    const open = () => {
      if (!this.editor.isEditable) return false
      openRecordPicker(this.editor)
      return true
    }
    // The console's ⌘K answers to either key on every platform, so the
    // editor claims both, or one of them would reach the palette.
    return { "Meta-k": open, "Ctrl-k": open }
  },
})

/** Everything the editor knows: the Markdown blocks and marks, record links,
 * the two menus and ⌘K. The parser and the serializer are built from the same
 * list, so a construct the editor offers is one it can store. */
export function markdownExtensions(placeholder = "") {
  return [
    StarterKit.configure({
      // Markdown has no underline.
      underline: false,
      link: { openOnClick: false, autolink: true },
    }),
    TaskList,
    TaskItem.configure({ nested: true }),
    TableKit.configure({ table: { resizable: false } }),
    Placeholder.configure({ placeholder }),
    RecordLink,
    SlashCommands,
    RecordMentions,
    RecordShortcut,
    Markdown,
  ]
}

/** The document as the Markdown a property stores. The serializer sets some
 * blocks (a table) apart with blank lines even at the document's edges, and a
 * record link carries the space typed after it to the line's end; neither is
 * the writer's, and a trailing space turns the value's YAML into a quoted
 * string. */
export function storedMarkdown(editor: Editor): string {
  return editor
    .getMarkdown()
    .replace(/(\]\(substrate:\/\/[^)\s]+\)) +(?=\n|$)/g, "$1")
    .replace(/^\n+|\n+$/g, "")
}
