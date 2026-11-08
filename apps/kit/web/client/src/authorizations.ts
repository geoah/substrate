/** The app authorization's wire shapes (internal/substrate's authorize.go):
 * what an app's request asks the approval to set up, and what the server's
 * check says of a token now. */

/** One entry of an app's `authorization_details` (RFC 9396): a package the
 * app needs at a version or later, an account it delivers under, or a
 * webhook it delivers to. The token response answers the same shape, with
 * `version` the version now held. */
export interface AuthorizationDetail {
  type: "package" | "account" | "webhook"
  package?: string
  version?: number
  kind?: string
  id?: string
  properties?: Record<string, unknown>
  trigger?: string
  url?: string
}

/** `POST /api/v1/authorizations/check`'s body: the request the app would
 * send now, and the build's version, which the server records on the token
 * so an upgrade that would break this build is held while it is connected
 * (decision 0213). */
export interface AuthorizationCheck {
  scope: string
  details: AuthorizationDetail[]
  softwareVersion?: string
}

/** Why a setup row cannot run: a reason a client branches on, the server's
 * sentence for it, and the console path where the owner resolves it. */
export interface AuthorizationBlock {
  reason: string
  problem: string
  fix?: string
}

export interface AuthorizationSetup {
  type: string
  /** `install`, `upgrade`, `create`, `enable`, `none`, `wait`, or `unknown`
   * for a row the token cannot see. */
  action: string
  blocked?: AuthorizationBlock
  progress?: string
  package?: {
    id: string
    required: number
    held?: number
    shipped?: number
  }
  account?: { kind: string; id?: string; properties?: Record<string, unknown> }
  webhook?: { trigger: string; package?: string }
}

/** Whether a token still has what its app needs: the requested scopes it
 * lacks, each detail's action and when the token ends. */
export interface AuthorizationCheckResult {
  scopes: { missing: string[] }
  setup: AuthorizationSetup[]
  expiresAt?: string
}

/** The actions approving does something for. */
export const setupActionsNeedingApproval = [
  "install",
  "upgrade",
  "create",
  "enable",
] as const

/** The block reason only a newer server lifts. */
export const blockServerUpdate = "server-update"
