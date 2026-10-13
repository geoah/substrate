/** The shipped kinds and the new-record sheet drawn for one of them, shared
 * by create-sheet.test.tsx and the four files of the every-kind sweep.
 *
 * The sweep is cut in four because vitest runs the files of a run side by
 * side and the tests of one file in order: as one file it was 43 to 80 s of
 * a 68 to 120 s `console test` job on CI, and every other file waited on
 * it. Kind `i` lands in file `i % SWEEP_FILES`, so the widest kinds, which
 * sort next to each other, spread over the four.
 *
 * Plain `.ts` with `createElement`: a `.tsx` module that exports anything
 * but components trips react-refresh's lint. A test file importing this
 * mocks `@tanstack/react-router` itself, before the import, because the
 * sheet links to records. */

import { createElement } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { parseAllDocuments } from "yaml"
import { afterEach, describe, expect, it } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { KindInfo } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { templateYAML } from "@/lib/record-yaml"

import { CreateSheet } from "./create-sheet"

/** How many files the sweep is cut into: create-sheet.kinds-1.test.tsx to
 * create-sheet.kinds-4.test.tsx. */
export const SWEEP_FILES = 4

const files = import.meta.glob<string>(
  ["../../../../../kinds/**/*.yaml", "../../../../../samples/**/*.yaml"],
  { query: "?raw", import: "default", eager: true }
)

/** Every shipped kind as the registry lists it: the declaration's `data` is
 * the definition. */
function shipped(): KindInfo[] {
  const out: KindInfo[] = []
  for (const source of Object.values(files)) {
    for (const doc of parseAllDocuments(source)) {
      const d = doc.toJS() as {
        kind?: string
        metadata?: { id?: string }
        data?: Record<string, unknown>
      } | null
      if (d?.kind !== "substrate.reamde.dev/core/kind" || !d.data) continue
      const identity = d.metadata?.id ?? ""
      const [authority = "", pkg = "", name = ""] = identity.split("/")
      out.push({
        identity,
        name,
        authority,
        package: pkg,
        version: 1,
        source: "installed",
        description:
          typeof d.data.description === "string" ? d.data.description : "",
        definition: d.data,
      })
    }
  }
  return out.sort((a, b) => a.identity.localeCompare(b.identity))
}

export const KINDS = shipped()

export function kindOf(identity: string): KindInfo {
  const kind = KINDS.find((k) => k.identity === identity)
  if (!kind) throw new Error(`no shipped kind ${identity}`)
  return kind
}

/** The properties the sheet shows, in order. */
export function shownRows(): string[] {
  return [
    ...document.querySelectorAll("[data-slot=property-sheet] [data-property]"),
  ].map((el) => el.getAttribute("data-property") ?? "")
}

export function renderSheet(
  kind: KindInfo,
  kinds: KindInfo[] = KINDS,
  technical = true,
  text = templateYAML(kind)
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, enabled: false } },
  })
  return render(
    createElement(
      ConsolePreferencesContext.Provider,
      {
        value: {
          preferences: {
            collapsed: [],
            favorites: [],
            sidebarOpen: true,
            ...DEFAULT_SETTINGS,
            technicalDetails: technical,
          },
          busy: false,
          change: () => {},
          set: () => {},
        },
      },
      createElement(
        QueryClientProvider,
        { client },
        createElement(CreateSheet, {
          text,
          kind,
          kinds,
          onChange: () => {},
        })
      )
    )
  )
}

/** Opens the fold, where there is one. */
export function openFold() {
  const more = screen.queryByRole("button", { name: /\d+ more:/ })
  if (more) fireEvent.click(more)
}

/** Declares file `file` (1 to SWEEP_FILES) of the sweep: every shipped kind
 * in it drawn, its fold opened, and every row's editor opened. */
export function sweepKinds(file: number) {
  const mine = KINDS.filter((_, i) => i % SWEEP_FILES === file - 1)

  describe("the create sheet", () => {
    afterEach(cleanup)

    // The widest provider kinds (Slack file, Linear issue) take 6 to 12 s on
    // a CI runner, past vitest's 5 s default, and the time grows with the
    // kind. The sweep gets its own budget.
    it.each(mine.map((k) => [k.identity, k] as const))(
      "%s draws every row, the folded ones too, and opens every editor",
      (_, kind) => {
        renderSheet(kind)
        openFold()
        expect(screen.queryByRole("button", { name: /\d+ more:/ })).toBeNull()
        // Untouched, nothing is wrong yet.
        expect(screen.queryAllByRole("alert")).toEqual([])
        const rows = [...document.querySelectorAll("[data-property]")].map(
          (el) => el.getAttribute("data-property") ?? ""
        )
        for (const name of rows) {
          const edit = document.querySelector(
            `[data-property="${name}"] [role=button]`
          )
          if (edit) fireEvent.click(edit)
        }
      },
      30_000
    )
  })
}
