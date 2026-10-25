/** What a refused ink save says. A conflict (another save landed after
 * editing began) keeps the editor open on the drawing and offers it as a
 * download, rather than send the reader to reload, which would lose it. */

import { writeError } from "@/components/property-sheet/use-record-patch"
import { ApiError } from "@/lib/api/types"
import type { InkDocument } from "@/lib/ink"

/** A refused save: what it says, and, when another save landed since
 * editing began, the drawing that was not stored, so it can be kept. */
export interface InkSaveFailure {
  message: string
  draft?: InkDocument
}

/** What a refused save of `draft` says. A conflict keeps the editor open on
 * the draft rather than send the reader to reload, which would lose it. */
export function inkSaveFailure(
  error: unknown,
  draft: InkDocument
): InkSaveFailure {
  if (error instanceof ApiError && error.code === "conflict")
    return {
      message:
        "This ink was not saved: another save changed this record after you started drawing. Your drawing is still open. Download it to keep it, then cancel to see the saved version.",
      draft,
    }
  if (error instanceof ApiError && error.status === 413)
    return {
      message:
        "This note is larger than the 64 MiB a substrate stores in one file.",
    }
  return { message: writeError(error) }
}
