/** Changelog (`/changelog`): "what is happening right now / around time T?" —
 * the repository's own log, read through `GET /substrate.reamde.dev/core/changes`, as ONE
 * FLAT table on the shared table system (owner ruling 2026-08-06; the intent
 * fold is gone), facet-filtered server-side, expandable per row to a human
 * summary of the write, tailed live via Follow, paged prev/next by seq cursor
 * (the changelog has no offset cursor — recorded deviation, ticket 009).
 *
 * The surface was called "Stream" until the owner pointed out that the thing it
 * reads IS the log — so it is named for what it shows. */

import { ChangelogPanel } from "@/components/changelog/changelog-panel"

export function ChangelogPage() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <h1 className="text-lg font-semibold">Changelog</h1>
        <p className="text-xs text-muted-foreground">
          Every commit in the substrate, newest first — one row per change,
          expandable to what it did.
        </p>
      </div>
      <ChangelogPanel />
    </div>
  )
}
