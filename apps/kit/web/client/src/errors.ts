export interface ProblemDetail {
  path?: string
  message?: string
  [key: string]: unknown
}

/** The REST envelope's `{error: {code, message, problems, problemDetails}}`
 * with the HTTP status that carried it, or a transport failure in the same
 * shape so a screen says one thing for both. */
export class SubstrateError extends Error {
  readonly code: string
  readonly status: number
  readonly problems: string[]
  readonly problemDetails: ProblemDetail[]
  /** Seconds the server asked the client to wait (`Retry-After`), on a 429
   * or a 503. */
  readonly retryAfter?: number
  /** A 403 whose `WWW-Authenticate` names `insufficient_scope`: the token
   * lacks a scope the request needs, which only a new approval gives. */
  readonly insufficientScope: boolean
  /** The envelope's machine-readable reason beside `code`, where the server
   * gives one: `stale_revision` (a newer write of the record landed first),
   * `in_doubt` (the write may have landed), `first_attempt_landed` (a
   * request repeated under an `Idempotency-Key` did not run, and the first
   * attempt's write stands), `first_attempt_running` (it did not run, and
   * the first attempt may still land), `not_permitted` (a rule of the
   * person's roles refused it), `quota`, `key_refused`,
   * `observed_rederiving`, `set_aside` (the server found the repository
   * damaged when it started). */
  readonly reason?: string
  /** On a `410 compacted`, the head and the history generation to start
   * over from, where the server gives them (an app's token gets neither). */
  readonly head?: number
  readonly generation?: string

  constructor(
    code: string,
    message: string,
    status: number,
    problems: string[] = [],
    details: ProblemDetail[] = [],
    extra: {
      retryAfter?: number
      insufficientScope?: boolean
      reason?: string
      head?: number
      generation?: string
    } = {}
  ) {
    super(message)
    this.name = "SubstrateError"
    this.code = code
    this.status = status
    this.problems = problems
    this.problemDetails = details
    this.retryAfter = extra.retryAfter
    this.insufficientScope = extra.insufficientScope ?? false
    this.reason = extra.reason
    this.head = extra.head
    this.generation = extra.generation
  }

  /** The words a screen shows: the message and any problems it lists. */
  get words(): string {
    return [this.message, ...this.problems].filter(Boolean).join(" ")
  }

  /** The server can no longer resume a cursor it was given: list again
   * from the first page. */
  get compacted(): boolean {
    return this.status === 410 && this.code === "compacted"
  }

  get conflict(): boolean {
    return this.status === 409 || this.status === 412
  }

  /** A refusal the same request may meet with another answer later: a busy
   * or failing server, never a refusal of the request itself. */
  get transient(): boolean {
    return this.status === 429 || this.status >= 500
  }
}

/** `Retry-After` as seconds: a delay, or an HTTP date from `now`. */
export function retryAfterSeconds(
  header: string | null,
  now = Date.now()
): number | undefined {
  if (!header) return undefined
  const text = header.trim()
  if (/^\d+$/.test(text)) return Number(text)
  const at = Date.parse(text)
  if (Number.isNaN(at)) return undefined
  return Math.max(0, Math.ceil((at - now) / 1000))
}

export async function failure(res: Response): Promise<SubstrateError> {
  const extra = {
    retryAfter: retryAfterSeconds(res.headers.get("Retry-After")),
    insufficientScope:
      res.status === 403 &&
      /insufficient_scope/.test(res.headers.get("WWW-Authenticate") ?? ""),
  }
  const text = await res.text().catch(() => "")
  try {
    const body = JSON.parse(text) as {
      error?:
        | {
            code?: string
            reason?: string
            message?: string
            problems?: string[]
            problemDetails?: ProblemDetail[]
            head?: number
            generation?: string
          }
        | string
      error_description?: string
    }
    if (body.error && typeof body.error === "object") {
      const e = body.error
      return new SubstrateError(
        e.code ?? "internal",
        e.message ?? res.statusText,
        res.status,
        e.problems ?? [],
        e.problemDetails ?? [],
        {
          ...extra,
          reason: e.reason || undefined,
          head: e.head,
          generation: e.generation,
        }
      )
    }
    if (typeof body.error === "string") {
      return new SubstrateError(
        body.error,
        body.error_description ?? body.error,
        res.status,
        [],
        [],
        extra
      )
    }
  } catch {
    // not JSON: fall through to the status line
  }
  return new SubstrateError(
    "http",
    `The server answered ${res.status} ${res.statusText}`.trim(),
    res.status,
    [],
    [],
    extra
  )
}

/** Whether a thrown value is the network failing, not the server answering:
 * `fetch` rejects with a TypeError when no response came back. An abort the
 * caller asked for is not one. */
export function isNetworkError(error: unknown): boolean {
  return error instanceof TypeError
}

/** Any thrown value as words for a screen. */
export function describe(error: unknown): string {
  if (error instanceof SubstrateError) return error.words
  if (isNetworkError(error))
    return "Couldn't reach the server. Check your connection and try again."
  if (error instanceof Error) return error.message
  return String(error)
}
