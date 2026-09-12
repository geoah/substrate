/** What a one-page layout says when a cursor remained: the page is the first
 * `first` rows and the rest were not loaded. A board and a timeline stop at
 * one page by contract; this is how they stay honest about it. */

export function IncompleteNote({
  shown,
  first,
  unread = [],
}: {
  shown: number
  first: number
  /** A trait view's implementors whose read failed. */
  unread?: string[]
}) {
  if (!unread.length && shown < first) return null
  return (
    <p className="border-t px-4 py-3 text-center text-xs text-muted-foreground">
      {shown >= first && `Showing the first ${shown}; more were not loaded.`}
      {unread.length > 0 && <> Could not read {unread.join(", ")}.</>}
    </p>
  )
}
