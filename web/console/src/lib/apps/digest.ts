/** What a mounted guest is keyed on: a hash of the code it runs. Saving the
 * app record through `apply`, the editor or an accepted proposal invalidates
 * the app query; only a changed digest remounts, so a rename or a new grant
 * leaves a running app alone. The modules are folded in name order, so the
 * digest does not move with jsonb's key order. */

export async function digest(
  source: string,
  modules: Record<string, string> = {}
): Promise<string> {
  const names = Object.keys(modules).sort()
  const text = JSON.stringify([source, names.map((n) => [n, modules[n]])])
  const bytes = new TextEncoder().encode(text)
  const hash = await crypto.subtle.digest("SHA-256", bytes)
  return Array.from(new Uint8Array(hash), (b) =>
    b.toString(16).padStart(2, "0")
  ).join("")
}
