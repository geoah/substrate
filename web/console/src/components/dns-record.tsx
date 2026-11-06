/** One DNS record as a person types it into their DNS provider: the name, the
 * type and the value, each labeled. The register page shows its shape before
 * the proof exists, and Settings shows the exact record to copy.
 *
 * The name and the value stay on one line, cut with an ellipsis: a TXT value
 * is one long token, and wrapped it reads as several. The full text is the
 * tooltip and the copy button's. */

import { CopyButton } from "@/components/identity/copy-button"

export function DnsRecord({
  name,
  type,
  value,
}: {
  name: string
  type: string
  value: string
}) {
  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-1.5">
      <dt className="text-faint">Name</dt>
      <dd className="flex min-w-0 items-center gap-1.5 font-mono">
        <span className="min-w-0 truncate" title={name}>
          {name}
        </span>
        <CopyButton value={name} label="Copy the record name" />
      </dd>
      <dt className="text-faint">Type</dt>
      <dd className="font-mono">{type}</dd>
      <dt className="text-faint">Value</dt>
      <dd className="flex min-w-0 items-center gap-1.5 font-mono">
        <span className="min-w-0 truncate" title={value}>
          {value}
        </span>
        <CopyButton value={value} label="Copy the record value" />
      </dd>
    </dl>
  )
}
