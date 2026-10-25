import { DownloadIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { saveBytes } from "@/lib/api/blobs"
import { INK_MEDIA_TYPE, inkText } from "@/lib/ink"

import type { InkSaveFailure } from "./save-failure"

/** The line under an editor after a refused save. */
export function SaveFailureNote({
  failure,
  fileName,
}: {
  failure: InkSaveFailure
  fileName: string
}) {
  const { draft } = failure
  return (
    <>
      <span className="text-destructive" role="alert">
        {failure.message}
      </span>
      {draft && (
        <Button
          type="button"
          variant="link"
          size="sm"
          onClick={() => saveBytes(inkText(draft), fileName, INK_MEDIA_TYPE)}
        >
          <DownloadIcon data-icon="inline-start" />
          Download this drawing
        </Button>
      )}
    </>
  )
}
