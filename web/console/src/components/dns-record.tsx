/** One DNS record as a person types it into their DNS provider: the name, the
 * type and the value, each labeled. The register page shows its shape before
 * the proof exists, and Settings shows the exact record to copy. */

import { CopyButton } from "@/components/identity/copy-button"

export function DnsRecord({
  name,
  type,
  value,
  copyable = false,
}: {
  name: string
  type: string
  value: string
  /** Offer a copy button beside the name and the value. Off for a preview,
   * whose value is not yet the one to publish. */
  copyable?: boolean
}) {
  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5">
      <dt className="text-faint">Name</dt>
      <dd className="flex items-start gap-1.5 font-mono [overflow-wrap:anywhere]">
        {name}
        {copyable && <CopyButton value={name} label="Copy the record name" />}
      </dd>
      <dt className="text-faint">Type</dt>
      <dd className="font-mono">{type}</dd>
      <dt className="text-faint">Value</dt>
      <dd className="flex items-start gap-1.5 font-mono [overflow-wrap:anywhere]">
        {value}
        {copyable && <CopyButton value={value} label="Copy the record value" />}
      </dd>
    </dl>
  )
}
