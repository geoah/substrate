/** The two menus the editor opens at the cursor. `/` offers the Markdown
 * blocks and a link to a record of any collection; `@` (and ⌘K) searches the
 * records the way the ⌘K palette does and inserts the one picked as a record
 * link. Picking a collection under `/` opens the record search narrowed to
 * it. */

import { useEffect, useState } from "react"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import type { Editor, Range } from "@tiptap/core"
import {
  AtSignIcon,
  CodeIcon,
  Heading1Icon,
  Heading2Icon,
  Heading3Icon,
  ListChecksIcon,
  ListIcon,
  ListOrderedIcon,
  MinusIcon,
  PilcrowIcon,
  QuoteIcon,
  TableIcon,
} from "lucide-react"

import { KindGlyph } from "@/components/identity/kind-glyph"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordsQueryOptions, searchQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { collectionGroups } from "@/lib/collections"
import { matchesWordPrefixes, typeAheadQuery } from "@/lib/command-match"
import { kindByIdentity } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { displayName, displayPlural, untitled } from "@/lib/kind-names"
import { QUICK_PURPOSES, searchPurposes } from "@/lib/search"
import { openRecordPicker } from "./record-link"
import { SuggestionMenu, type MenuKeys, type MenuRow } from "./suggestion-menu"

/** What a suggestion hands the menu it opens. */
export interface MenuProps {
  editor: Editor
  range: Range
  query: string
  keysRef: MenuKeys
}

/** How long typing rests before the records are asked for. */
const DEBOUNCE_MS = 150
/** How many records the picker offers. */
const HITS = 8

interface Block {
  label: string
  /** Other words it answers to. */
  aliases: string
  icon: React.ReactNode
  hint?: string
  run: (editor: Editor) => void
}

const BLOCKS: Block[] = [
  {
    label: "Text",
    aliases: "paragraph plain",
    icon: <PilcrowIcon />,
    run: (e) => e.chain().focus().setParagraph().run(),
  },
  {
    label: "Heading 1",
    aliases: "h1 title",
    icon: <Heading1Icon />,
    hint: "#",
    run: (e) => e.chain().focus().setHeading({ level: 1 }).run(),
  },
  {
    label: "Heading 2",
    aliases: "h2 subtitle",
    icon: <Heading2Icon />,
    hint: "##",
    run: (e) => e.chain().focus().setHeading({ level: 2 }).run(),
  },
  {
    label: "Heading 3",
    aliases: "h3",
    icon: <Heading3Icon />,
    hint: "###",
    run: (e) => e.chain().focus().setHeading({ level: 3 }).run(),
  },
  {
    label: "Bulleted list",
    aliases: "ul unordered bullet",
    icon: <ListIcon />,
    hint: "-",
    run: (e) => e.chain().focus().toggleBulletList().run(),
  },
  {
    label: "Numbered list",
    aliases: "ol ordered",
    icon: <ListOrderedIcon />,
    hint: "1.",
    run: (e) => e.chain().focus().toggleOrderedList().run(),
  },
  {
    label: "To-do list",
    aliases: "todo task checkbox check",
    icon: <ListChecksIcon />,
    hint: "[ ]",
    run: (e) => e.chain().focus().toggleTaskList().run(),
  },
  {
    label: "Quote",
    aliases: "blockquote citation",
    icon: <QuoteIcon />,
    hint: ">",
    run: (e) => e.chain().focus().setBlockquote().run(),
  },
  {
    label: "Code block",
    aliases: "pre snippet fence",
    icon: <CodeIcon />,
    hint: "```",
    run: (e) => e.chain().focus().setCodeBlock().run(),
  },
  {
    label: "Table",
    aliases: "grid columns",
    icon: <TableIcon />,
    run: (e) =>
      e
        .chain()
        .focus()
        .insertTable({ rows: 3, cols: 3, withHeaderRow: true })
        .run(),
  },
  {
    label: "Divider",
    aliases: "hr rule separator line",
    icon: <MinusIcon />,
    hint: "---",
    run: (e) => e.chain().focus().setHorizontalRule().run(),
  },
]

export function SlashMenu({ editor, range, query, keysRef }: MenuProps) {
  const [technical] = useTechnicalDetails()
  const registry = useQuery(kindsQueryOptions)
  const kinds = collectionGroups(registry.data ?? []).flatMap((g) =>
    technical ? [...g.primary, ...g.hidden] : g.primary
  )
  // Replace `/query` with what was picked.
  const take = (run: (e: Editor) => void) => () => {
    editor.chain().focus().deleteRange(range).run()
    run(editor)
  }
  const rows: MenuRow[] = [
    ...BLOCKS.filter((b) =>
      matchesWordPrefixes(query, `${b.label} ${b.aliases}`)
    ).map((b) => ({
      key: `block:${b.label}`,
      group: "Blocks",
      icon: b.icon,
      label: b.label,
      hint: b.hint,
      pick: take(b.run),
    })),
    ...(matchesWordPrefixes(query, "link record mention page")
      ? [
          {
            key: "link:any",
            group: "Link to a record",
            icon: <AtSignIcon />,
            label: "Any record",
            hint: "@",
            pick: take((e) => openRecordPicker(e)),
          },
        ]
      : []),
    ...kinds
      .filter((k) =>
        matchesWordPrefixes(
          query,
          `${displayName(k)} ${displayPlural(k)} link record`
        )
      )
      .map((k) => ({
        key: `link:${k.identity}`,
        group: "Link to a record",
        icon: <KindGlyph kind={k} size="xs" />,
        label: displayName(k),
        hint: technical ? k.identity : undefined,
        pick: take((e) => openRecordPicker(e, k.identity)),
      })),
  ]
  return (
    <SuggestionMenu
      label="Insert a block or a link"
      rows={rows}
      keysRef={keysRef}
      empty="Nothing matches"
    />
  )
}

function useDebounced(value: string, ms: number): string {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), ms)
    return () => clearTimeout(timer)
  }, [value, ms])
  return settled
}

export function RecordMenu({ editor, range, query, keysRef }: MenuProps) {
  const [technical] = useTechnicalDetails()
  // The kind a `/` pick narrowed to, read once: the picker keeps it while open.
  const [kind] = useState<string | undefined>(
    () => editor.storage.recordLink.kind
  )
  const registry = useQuery(kindsQueryOptions)
  // Anything typed is a search, even before the pause lets it be asked:
  // Enter must never pick a recent record the reader has typed past.
  const searching = typeAheadQuery(query).length > 0
  const asked = typeAheadQuery(useDebounced(query, DEBOUNCE_MS))
  const kinds = kind ? [kind] : undefined
  const found = useQuery({
    ...searchQueryOptions(asked, {
      mode: "lexical",
      kinds,
      purposes: searchPurposes({
        narrowed: Boolean(kind),
        quick: true,
        technical,
        includeSystem: false,
      }),
      first: HITS,
    }),
    placeholderData: keepPreviousData,
  })
  // With nothing typed, the records changed last.
  const recent = useQuery({
    ...recordsQueryOptions({
      kinds: kinds ?? [],
      filter: kind ? undefined : { purposes: QUICK_PURPOSES },
      orderBy: "updatedAt:desc",
      first: HITS,
    }),
    enabled: !searching,
  })
  const records: SubstrateRecord[] = searching
    ? asked
      ? (found.data?.records ?? [])
      : []
    : (recent.data?.records ?? [])
  const waiting = searching ? !asked || found.isFetching : recent.isPending
  const known = kind ? kindByIdentity(registry.data ?? [], kind) : undefined
  const group = searching
    ? kind
      ? displayPlural(known ?? kind)
      : "Records"
    : kind
      ? `Recent ${displayPlural(known ?? kind).toLowerCase()}`
      : "Recently changed"

  const rows: MenuRow[] = records.map((r) => {
    const title = recordTitle(r.properties ?? {})
    return {
      key: `${r.kind}/${r.id}`,
      group,
      icon: <KindGlyph kind={r.kind} size="xs" />,
      label: title || (
        <span className="text-muted-foreground">{untitled(r.kind)}</span>
      ),
      hint: kind ? undefined : displayName(r.kind),
      pick: () =>
        editor
          .chain()
          .focus()
          .deleteRange(range)
          .insertRecordLink({ kind: r.kind, id: r.id, title })
          .run(),
    }
  })
  return (
    <SuggestionMenu
      label="Link to a record"
      rows={rows}
      keysRef={keysRef}
      busy={waiting}
      empty={
        waiting
          ? "Loading records…"
          : searching
            ? `No records match “${query}”`
            : "No records yet"
      }
    />
  )
}
