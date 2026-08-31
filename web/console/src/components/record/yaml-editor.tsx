/** The record editor's YAML lens: CodeMirror over the apply-able envelope, with
 * the KIND's declaration behind every affordance an editor is expected to have.
 *
 * - **Completion** (`completionsAt`) offers what may be written where the cursor
 *   is: the envelope's own keys at the top, the declared properties under
 *   `data.properties` (each with its datatype and one-liner, and never one that
 *   is already written), an enum's admitted values after its key, a state
 *   machine's states, the declared edge rels after a `rel:`. Typing raises it;
 *   Ctrl-Space asks for it.
 * - **Diagnostics** are `validateApplyDoc`'s problems, underlined on the line
 *   they belong to and marked in the gutter, so a bad datatype is a squiggle
 *   under the value rather than a 422 after the round trip.
 * - **Hover** a line that writes a declared property and the declaration
 *   answers: datatype, required, the one-liner, admitted values, an example.
 *
 * The tint is CodeMirror's own (lezer-yaml), coloured with the app's
 * `--shiki-token-*` variables — the same values the manifest view uses, so one
 * YAML reads alike on both surfaces and follows light/dark for free. */

import { useEffect, useMemo, useRef } from "react"
import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completionKeymap,
  type CompletionContext,
  type CompletionResult,
} from "@codemirror/autocomplete"
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands"
import { yaml as yamlLanguage } from "@codemirror/lang-yaml"
import {
  HighlightStyle,
  bracketMatching,
  indentOnInput,
  indentUnit,
  syntaxHighlighting,
} from "@codemirror/language"
import { lintGutter, linter, type Diagnostic } from "@codemirror/lint"
import { EditorState, type Extension } from "@codemirror/state"
import {
  EditorView,
  drawSelection,
  highlightActiveLine,
  highlightActiveLineGutter,
  hoverTooltip,
  keymap,
  lineNumbers,
} from "@codemirror/view"
import { tags } from "@lezer/highlight"

import type { KindInfo } from "@/lib/api/types"
import {
  exampleFor,
  propSpecs,
  systemSpecs,
  typeLabel,
  type PropSpec,
} from "@/lib/record-schema"
import { validateApplyDoc, type ApplyContext } from "@/lib/record-yaml"
import { completionsAt } from "@/lib/yaml-completion"
import { specOnLine } from "@/lib/yaml-annotations"

const HIGHLIGHT = HighlightStyle.define([
  {
    tag: [tags.propertyName, tags.definition(tags.propertyName), tags.keyword],
    color: "var(--shiki-token-keyword)",
  },
  {
    tag: [tags.string, tags.content, tags.name],
    color: "var(--shiki-token-string-expression)",
  },
  {
    tag: [tags.number, tags.bool, tags.null, tags.atom],
    color: "var(--shiki-token-constant)",
  },
  {
    tag: tags.comment,
    color: "var(--shiki-token-comment)",
    fontStyle: "italic",
  },
  {
    tag: [tags.punctuation, tags.separator, tags.meta],
    color: "var(--shiki-token-punctuation)",
  },
])

const THEME = EditorView.theme({
  "&": {
    height: "100%",
    color: "var(--shiki-foreground)",
    backgroundColor: "transparent",
    fontFamily: 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, monospace',
    fontSize: "0.82rem",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "inherit", lineHeight: "1.7" },
  ".cm-content": { padding: "12px 0", caretColor: "var(--foreground)" },
  ".cm-gutters": {
    backgroundColor: "transparent",
    color: "var(--muted-foreground)",
    border: "none",
    borderRight: "1px solid var(--border)",
  },
  ".cm-activeLine": {
    backgroundColor: "color-mix(in oklab, var(--muted) 45%, transparent)",
  },
  ".cm-activeLineGutter": {
    backgroundColor: "transparent",
    color: "var(--foreground)",
  },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "color-mix(in oklab, var(--primary) 22%, transparent)",
  },
  ".cm-cursor": { borderLeftColor: "var(--foreground)" },
  ".cm-tooltip": {
    backgroundColor: "var(--popover)",
    color: "var(--popover-foreground)",
    border: "1px solid var(--border)",
    borderRadius: "0.375rem",
    boxShadow: "0 4px 12px rgb(0 0 0 / 0.12)",
  },
  ".cm-tooltip-autocomplete > ul > li": {
    fontFamily: "inherit",
    padding: "2px 6px",
  },
  ".cm-tooltip-autocomplete > ul > li[aria-selected]": {
    backgroundColor: "color-mix(in oklab, var(--primary) 18%, transparent)",
    color: "var(--popover-foreground)",
  },
  ".cm-completionDetail": {
    color: "var(--muted-foreground)",
    fontStyle: "normal",
    marginLeft: "0.75rem",
  },
  ".cm-completionInfo": {
    maxWidth: "22rem",
    backgroundColor: "var(--popover)",
    border: "1px solid var(--border)",
    borderRadius: "0.375rem",
    padding: "0.5rem 0.625rem",
  },
  ".cm-diagnostic": { fontFamily: "inherit", padding: "0.25rem 0.5rem" },
  ".cm-diagnostic-error": { borderLeft: "3px solid var(--destructive)" },
  ".cm-diagnostic-warning": { borderLeft: "3px solid var(--warning)" },
  ".cm-lintRange-error": {
    backgroundImage: "none",
    textDecoration: "underline wavy var(--destructive)",
    textUnderlineOffset: "0.3em",
  },
  ".cm-lintRange-warning": {
    backgroundImage: "none",
    textDecoration: "underline wavy var(--warning)",
    textUnderlineOffset: "0.3em",
  },
})

export function YamlEditor({
  value,
  onChange,
  kind,
  ctx,
  label = "Record YAML",
}: {
  value: string
  onChange: (next: string) => void
  /** The kind being edited: every completion, diagnostic and hover reads it. */
  kind: KindInfo
  /** What the write knows beyond the text (the record being edited). */
  ctx?: ApplyContext
  label?: string
}) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)

  const specs = useMemo(
    () => [...propSpecs(kind), ...systemSpecs(kind)],
    [kind]
  )

  // The live props, read by extensions that outlive the render that made them.
  // They are refreshed in an effect, never during render: the editor is built
  // once, and what it reads must be what the last commit said.
  const latest = useRef({ kind, ctx, onChange, specs })
  useEffect(() => {
    latest.current = { kind, ctx, onChange, specs }
  })

  useEffect(() => {
    if (!host.current || view.current) return

    const complete = (context: CompletionContext): CompletionResult | null => {
      const line = context.state.doc.lineAt(context.pos)
      const found = completionsAt(
        context.state.doc.toString(),
        line.number - 1,
        context.pos - line.from,
        latest.current.kind
      )
      if (!found) return null
      // Nothing typed yet: only an explicit ask opens the list, so the popup
      // does not blink open on every newline.
      const typed = context.state.doc.sliceString(
        line.from + found.from,
        context.pos
      )
      if (!context.explicit && !typed) return null
      return {
        from: line.from + found.from,
        options: found.options.map((option) => ({
          label: option.label,
          detail: option.detail,
          info: option.info,
          apply: option.apply,
          type: option.type === "value" ? "constant" : "property",
        })),
        validFor: /^[\w.\-/]*$/,
      }
    }

    const diagnostics = (editor: EditorView): Diagnostic[] => {
      const doc = editor.state.doc
      const problems = validateApplyDoc(
        doc.toString(),
        latest.current.kind,
        latest.current.ctx
      )
      return problems.map((problem) => {
        const at = doc.line(Math.min(Math.max(problem.line ?? 1, 1), doc.lines))
        return {
          from: at.from,
          to: at.to,
          severity: problem.severity,
          // Messages are authored with backticks around identifiers; the
          // diagnostic panel is plain text, where they read as quotes.
          message: problem.message.replace(/`/g, "'"),
        }
      })
    }

    const hover = hoverTooltip((editor, pos) => {
      const line = editor.state.doc.lineAt(pos)
      const spec = specOnLine(
        line.text,
        new Map(latest.current.specs.map((s) => [s.name, s]))
      )
      if (!spec) return null
      return {
        pos: line.from,
        end: line.to,
        above: true,
        create: () => ({ dom: specCard(spec) }),
      }
    })

    const extensions: Extension[] = [
      lineNumbers(),
      lintGutter(),
      history(),
      drawSelection(),
      highlightActiveLine(),
      highlightActiveLineGutter(),
      indentOnInput(),
      bracketMatching(),
      closeBrackets(),
      indentUnit.of("  "),
      EditorState.tabSize.of(2),
      yamlLanguage(),
      syntaxHighlighting(HIGHLIGHT),
      THEME,
      // A long scalar (an agent's prompt) must wrap, not scroll sideways:
      // the manifest view wraps, and the two surfaces read alike.
      EditorView.lineWrapping,
      autocompletion({ override: [complete], icons: false }),
      linter(diagnostics, { delay: 250 }),
      hover,
      keymap.of([
        ...closeBracketsKeymap,
        ...defaultKeymap,
        ...historyKeymap,
        ...completionKeymap,
      ]),
      EditorView.contentAttributes.of({ "aria-label": label }),
      EditorView.updateListener.of((update) => {
        if (update.docChanged)
          latest.current.onChange(update.state.doc.toString())
      }),
    ]

    view.current = new EditorView({
      state: EditorState.create({ doc: value, extensions }),
      parent: host.current,
    })

    return () => {
      view.current?.destroy()
      view.current = null
    }
    // Created ONCE: rebuilding on a prop change would drop the cursor, the
    // selection and the undo history mid-edit. The props extensions read live
    // through `latest`, and the document is fed by the effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // The document belongs to the page, not to the editor: a change from the FORM
  // lens (or a reseed) is dispatched in. An edit made HERE is already the same
  // text, so it never round-trips.
  useEffect(() => {
    const editor = view.current
    if (!editor || editor.state.doc.toString() === value) return
    editor.dispatch({
      changes: { from: 0, to: editor.state.doc.length, insert: value },
    })
  }, [value])

  return <div ref={host} className="h-full min-h-0 overflow-auto" />
}

/** What the kind says about the property on the hovered line. CodeMirror owns
 * this node, so the card is built rather than rendered. */
function specCard(spec: PropSpec): HTMLElement {
  const dom = document.createElement("div")
  dom.className = "flex max-w-sm flex-col gap-1 px-3 py-2 text-xs"

  const head = document.createElement("div")
  head.className = "flex items-center gap-2"
  const name = document.createElement("span")
  name.className = "data font-medium"
  name.textContent = spec.name
  const type = document.createElement("span")
  type.className = "data text-muted-foreground"
  type.textContent = typeLabel(spec)
  const need = document.createElement("span")
  need.className = spec.required
    ? "text-[10px] uppercase text-destructive"
    : "text-[10px] uppercase text-muted-foreground"
  need.textContent = spec.required ? "required" : "optional"
  head.append(name, type, need)
  dom.append(head)

  const line = (text: string, mono = false) => {
    const p = document.createElement("p")
    if (mono) p.className = "data text-muted-foreground"
    p.textContent = text
    dom.append(p)
  }

  if (spec.description) line(spec.description)
  if (spec.values?.length) {
    line(`one of: ${spec.values.map((v) => v.value).join(", ")}`, true)
  } else if (spec.states?.length) {
    line(`states: ${spec.states.join(", ")}`, true)
  } else {
    const example = exampleFor(spec)
    if (example) line(`e.g. ${example}`, true)
  }
  return dom
}
