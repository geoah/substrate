/** A record referenced from somewhere else. Kept as the name existing
 * surfaces import; the mark itself is `RecordRef`. */

import { RecordRef } from "@/components/identity/record-ref"

export function RecordPill({
  kind,
  id,
  title,
  className,
}: {
  /** The record's kind reference, `<authority>/<package>/<name>`. */
  kind: string
  id: string
  /** The record's display title; read when absent. */
  title?: string
  className?: string
}) {
  return <RecordRef kind={kind} id={id} title={title} className={className} />
}
