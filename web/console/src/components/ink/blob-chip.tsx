/** A stored file in one line: its name, size and media type, and a button
 * that downloads its bytes. What a `blobref` reads as wherever no richer
 * view of it exists. */

import { useState } from "react"
import { DownloadIcon, FileIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { fetchBlob, saveBytes } from "@/lib/api/blobs"
import type { BlobInfo } from "@/lib/api/types"
import { byteSize } from "@/lib/ink-record"

export function BlobChip({ blob }: { blob: BlobInfo }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | undefined>()
  return (
    <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
      <FileIcon className="size-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate">{blob.name ?? "Unnamed file"}</span>
      <span className="text-muted-foreground">
        {byteSize(blob.size)}
        {blob.mediaType ? ` · ${blob.mediaType}` : ""}
      </span>
      {blob.status === "stored" && (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          disabled={busy}
          onClick={() => {
            setBusy(true)
            setError(undefined)
            fetchBlob(blob.digest)
              .then((bytes) =>
                saveBytes(bytes, blob.name ?? blob.digest, blob.mediaType)
              )
              .catch((e: unknown) =>
                setError(e instanceof Error ? e.message : String(e))
              )
              .finally(() => setBusy(false))
          }}
        >
          <DownloadIcon data-icon="inline-start" />
          Download
        </Button>
      )}
      {blob.status !== "stored" && (
        <span className="text-muted-foreground">({blob.status})</span>
      )}
      {error && <span className="text-xs text-destructive">{error}</span>}
    </span>
  )
}
