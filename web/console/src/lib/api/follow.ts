/** A watch for a surface that re-reads through its ordinary queries, which
 * recovers from a `compacted` on its own: told its position is gone, it opens
 * again at the server's head and calls `relist` once that stream is live.
 * The server fixes a stream's head before it answers, so a read sent then
 * misses nothing, where one sent beside the stream could answer first and
 * leave a row committed between the two unheard. Every stream it opens is
 * the same reader under RULE W (compacted.ts), by `reader.key`: a refusal
 * within a minute of the start of the last recovery is `stopped`, and one
 * later recovers again. It lives apart from changes.ts so a test that stands
 * in for `watchChanges` drives it too. */

import {
  watchChanges,
  type ChangeFeedFilter,
  type WatchHandle,
  type WatchStatus,
} from "./changes"
import type { ResumingReader } from "./compacted"
import type { ChangeRow } from "./types"

export function followChanges(opts: {
  /** Resume above this seq, under `generation`; absent = the server's head. */
  from?: number
  generation?: string
  filter?: ChangeFeedFilter
  onRow: (row: ChangeRow) => void
  onStatus?: (status: WatchStatus, detail?: string) => void
  /** The surface under RULE W: the key of its allowance, the drop of what
   * it saved and how it shows the failure. */
  reader: ResumingReader
  /** Read again what the surface shows: the gap's rows are never told. */
  relist: () => void
}): WatchHandle {
  let stopped = false
  let handle = open(false)

  function open(recovering: boolean): WatchHandle {
    let pending = recovering
    return watchChanges({
      from: recovering ? undefined : opts.from,
      generation: recovering ? undefined : opts.generation,
      filter: opts.filter,
      onRow: opts.onRow,
      reader: opts.reader,
      onStatus: (status, detail) => {
        opts.onStatus?.(status, detail)
        if (status !== "live" || stopped || !pending) return
        pending = false
        opts.relist()
      },
      onCompacted: () => {
        if (!stopped) handle = open(true)
      },
    })
  }

  return {
    stop: () => {
      stopped = true
      handle.stop()
    },
  }
}
