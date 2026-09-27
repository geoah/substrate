/** The recovery export (`GET /api/v1/export`, decision 0069): the
 * repository's directory as of one committed point, as a tar. The token is the
 * whole credential, as it is for every record the archive carries. */

import { envelopeError, rootPath } from "./http"
import { getToken, sessionExpired } from "./session"

const FALLBACK_NAME = "substrate-export.tar"

/** The file name the server offered, or a plain one. */
export function exportFileName(disposition: string | null): string {
  const match = disposition?.match(/filename\*?=(?:UTF-8'')?"?([^";]+)"?/i)
  return match?.[1] ? decodeURIComponent(match[1]) : FALLBACK_NAME
}

/** Read the archive and hand it to the browser as a download. */
export async function downloadExport(): Promise<string> {
  const headers: Record<string, string> = { "X-Substrate-Actor": "console" }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(rootPath("export"), { headers })
  if (!res.ok) {
    if (res.status === 401) sessionExpired()
    let body: unknown
    try {
      body = await res.json()
    } catch {
      body = undefined
    }
    throw envelopeError(res.status, body)
  }
  const name = exportFileName(res.headers.get("Content-Disposition"))
  const url = URL.createObjectURL(await res.blob())
  try {
    const link = document.createElement("a")
    link.href = url
    link.download = name
    document.body.appendChild(link)
    link.click()
    link.remove()
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
  }
  return name
}
