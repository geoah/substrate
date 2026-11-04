/** A record name split for a DNS provider that appends the zone itself,
 * assuming the zone is the name's last two labels: `_substrate.home` in
 * `example.org` for `_substrate.home.example.org`. The zone a name sits in is
 * not knowable from the name (`example.co.uk`, a delegated subdomain), so the
 * console states the zone it assumed beside the shortened name rather than
 * offering the shortened name alone. Null where there is nothing to shorten. */
export function relativeRecordName(
  name: string
): { zone: string; relative: string } | null {
  const labels = name.replace(/\.$/, "").split(".")
  if (labels.length < 3) return null
  return {
    zone: labels.slice(-2).join("."),
    relative: labels.slice(0, -2).join("."),
  }
}

/** Whether name is exactly one label under host, `ada.example.com` under
 * `example.com`: the console's mirror of internal/api's underHost. */
export function underHost(name: string, host: string): boolean {
  const h = host.replace(/\.$/, "").toLowerCase()
  if (!h || !name.endsWith("." + h)) return false
  const label = name.slice(0, -(h.length + 1))
  return label !== "" && !label.includes(".")
}
