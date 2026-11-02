/** Domain verification (decision 0208): a repository proves it controls its
 * name's DNS after registration. Start answers the TXT record to publish,
 * minting its one-time proof when none is pending, and verify reads the record
 * back. Both are the owner's routes. */

import { request, rootPath } from "./http"
import type { DomainCheck, DomainProof } from "./types"

export function startDomainVerification(): Promise<DomainProof> {
  return request<DomainProof>("POST", rootPath("repository", "domain"))
}

export function verifyDomain(): Promise<DomainCheck> {
  return request<DomainCheck>(
    "POST",
    rootPath("repository", "domain", "verify")
  )
}
