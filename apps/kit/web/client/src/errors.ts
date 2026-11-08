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

  constructor(
    code: string,
    message: string,
    status: number,
    problems: string[] = [],
    details: ProblemDetail[] = []
  ) {
    super(message)
    this.name = "SubstrateError"
    this.code = code
    this.status = status
    this.problems = problems
    this.problemDetails = details
  }

  /** The words a screen shows: the message and any problems it lists. */
  get words(): string {
    return [this.message, ...this.problems].filter(Boolean).join(" ")
  }

  get conflict(): boolean {
    return this.status === 409 || this.status === 412
  }
}

export async function failure(res: Response): Promise<SubstrateError> {
  const text = await res.text().catch(() => "")
  try {
    const body = JSON.parse(text) as {
      error?:
        | {
            code?: string
            message?: string
            problems?: string[]
            problemDetails?: ProblemDetail[]
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
        e.problemDetails ?? []
      )
    }
    if (typeof body.error === "string") {
      return new SubstrateError(
        body.error,
        body.error_description ?? body.error,
        res.status
      )
    }
  } catch {
    // not JSON: fall through to the status line
  }
  return new SubstrateError(
    "http",
    `The server answered ${res.status} ${res.statusText}`.trim(),
    res.status
  )
}

/** Any thrown value as words for a screen. */
export function describe(error: unknown): string {
  if (error instanceof SubstrateError) return error.words
  if (error instanceof TypeError)
    return "Couldn't reach the server. Check your connection and try again."
  if (error instanceof Error) return error.message
  return String(error)
}
