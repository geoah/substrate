import type { BundleCodeChange } from "@/lib/api/types"

export function OAuthHosts({ hosts }: { hosts?: string[] }) {
  if (!hosts?.length) return null
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-medium">OAuth endpoint hosts</h3>
      <ul className="space-y-1 text-sm">
        {hosts.map((host) => (
          <li className="font-mono break-all" key={host}>
            {host}
          </li>
        ))}
      </ul>
    </section>
  )
}

export function CodeChanges({ changes }: { changes?: BundleCodeChange[] }) {
  if (!changes?.length) return null
  return (
    <section className="space-y-3">
      <h3 className="text-sm font-medium">Code changes to review</h3>
      {changes.map((change) => (
        <details key={`${change.kind}/${change.id}`}>
          <summary className="text-sm break-all">
            {change.kind}: {change.id}
          </summary>
          <pre className="mt-2 max-h-96 overflow-auto text-xs break-all whitespace-pre-wrap">
            {change.diff}
          </pre>
        </details>
      ))}
    </section>
  )
}
