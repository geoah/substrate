import {
  requirementOf,
  unrevokedSentences,
  unrevokedTitle,
  type AppManifest,
  type Session,
} from "@substrate/auth"
import {
  ServerTooOld,
  describe,
  displayServerVersion,
  isRelease,
  locate,
  parseServerVersion,
  type LocatedServer,
} from "@substrate/client"
import { useId, useRef, useState, type FormEvent, type ReactNode } from "react"
import { Spinner } from "./SubstrateApp"
import { ProblemBox } from "./ui"

const lastServerKey = "substrate.lastServer"

type Phase =
  | { type: "idle"; retryable?: boolean }
  | { type: "checking" }
  | { type: "found"; located: LocatedServer }
  | { type: "tooOld"; error: ServerTooOld }
  | { type: "signingIn"; located: LocatedServer }

function shown(located: LocatedServer): string {
  return located.repository ?? new URL(located.server).host
}

/** The connect screen: the owner's repository or server, checked against
 * its discovery documents and the app's requirement, then the server's own
 * sign-in and approval page (the kit's ConnectController). A server too old
 * for the app says what it cannot do yet, with Try Again and Use Another
 * Server; one found says its host before the browser leaves for it. */
export function ConnectScreen(props: {
  manifest: AppManifest
  tagline?: string
  icon?: ReactNode
  /** A problem from before this screen: words alone are a sign-in that
   * came back refused; a title says what else it is. */
  problem?: string | { title: string; message: string } | null
  /** Tokens a sign-out could not revoke, said under the form. */
  unrevoked?: Session[]
  onSignIn: (located: LocatedServer) => Promise<void>
}) {
  const app = props.manifest.name
  const [typed, setTyped] = useState(() => {
    try {
      return localStorage.getItem(lastServerKey) ?? ""
    } catch {
      return ""
    }
  })
  const [phase, setPhase] = useState<Phase>({ type: "idle" })
  const [problem, setProblem] = useState<{
    title: string
    message: string
  } | null>(() =>
    typeof props.problem === "string"
      ? { title: "Couldn't sign in", message: props.problem }
      : (props.problem ?? null)
  )
  const input = useRef<HTMLInputElement>(null)
  const fieldID = useId()
  const noteID = useId()

  const check = async () => {
    setPhase({ type: "checking" })
    setProblem(null)
    try {
      const located = await locate(
        typed,
        undefined,
        requirementOf(props.manifest)
      )
      try {
        localStorage.setItem(lastServerKey, typed.trim())
      } catch {
        // a private window keeps nothing
      }
      setPhase({ type: "found", located })
    } catch (e) {
      if (e instanceof ServerTooOld) {
        setPhase({ type: "tooOld", error: e })
      } else {
        setProblem({
          title: "Couldn't reach that server",
          message: describe(e),
        })
        setPhase({ type: "idle", retryable: true })
      }
    }
  }

  const signIn = async (located: LocatedServer) => {
    setPhase({ type: "signingIn", located })
    setProblem(null)
    try {
      await props.onSignIn(located)
    } catch (e) {
      setProblem({ title: "Couldn't sign in", message: describe(e) })
      setPhase({ type: "found", located })
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (phase.type === "found") void signIn(phase.located)
    else if (phase.type === "idle") void check()
  }

  const changeServer = () => {
    setTyped("")
    setProblem(null)
    setPhase({ type: "idle" })
    input.current?.focus()
  }

  const edit = (text: string) => {
    setTyped(text)
    if (phase.type === "found" && text === shown(phase.located)) return
    if (phase.type === "found" || phase.type === "tooOld")
      setPhase({ type: "idle" })
    setProblem(null)
  }

  const busy = phase.type === "checking" || phase.type === "signingIn"
  const located =
    phase.type === "found" || phase.type === "signingIn"
      ? phase.located
      : undefined
  const host = located ? new URL(located.server).host : undefined
  const title =
    phase.type === "checking"
      ? "Checking…"
      : phase.type === "found"
        ? "Sign In with Your Substrate"
        : phase.type === "signingIn"
          ? "Signing In…"
          : phase.type === "idle" && phase.retryable
            ? "Try Again"
            : "Continue"
  const note = located
    ? `You sign in on ${host} itself and choose what ${app} may do there. To revoke it later, use ${host}'s Settings, under Apps.`
    : "Your repository's name, such as ada.example.com, or your Substrate server's address. A name finds its server on its own."
  const notices = unrevokedSentences(app, props.unrevoked ?? [])

  return (
    <main className="flex min-h-full items-center justify-center bg-grouped px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          {props.icon && <div className="mb-4">{props.icon}</div>}
          <h1 className="text-[28px] font-bold tracking-tight">{app}</h1>
          {props.tagline && (
            <p className="mt-2 text-[15px] text-secondary">{props.tagline}</p>
          )}
        </div>
        <form onSubmit={submit} className="space-y-4">
          <div className="overflow-hidden rounded-xl bg-card">
            <label
              className="block px-4 pt-3 text-[13px] text-secondary"
              htmlFor={fieldID}
            >
              Repository or server
            </label>
            <input
              ref={input}
              id={fieldID}
              aria-describedby={noteID}
              className="w-full bg-transparent px-4 pt-1 pb-3 text-[17px] outline-none placeholder:text-tertiary"
              placeholder="ada.example.com"
              autoComplete="url"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              inputMode="url"
              value={typed}
              disabled={busy}
              onChange={(e) => edit(e.target.value)}
              autoFocus
            />
          </div>

          {phase.type === "tooOld" ? (
            <TooOld
              app={app}
              error={phase.error}
              onTryAgain={() => void check()}
              onChangeServer={changeServer}
            />
          ) : (
            <>
              {located && (
                <dl className="overflow-hidden rounded-xl bg-card">
                  <div className="px-4 py-2.5">
                    <dt className="text-[13px] text-secondary">Server found</dt>
                    <dd className="text-[17px] break-all">{host}</dd>
                  </div>
                  {located.repository && (
                    <div className="border-t border-separator px-4 py-2.5">
                      <dt className="text-[13px] text-secondary">Repository</dt>
                      <dd className="text-[17px] break-all">
                        {located.repository}
                      </dd>
                    </div>
                  )}
                </dl>
              )}
              {problem && (
                <div className="overflow-hidden rounded-xl bg-card">
                  <ProblemBox
                    role="alert"
                    title={problem.title}
                    message={problem.message}
                  />
                </div>
              )}
              <button
                type="submit"
                disabled={busy || (!located && !typed.trim())}
                className="flex h-12 w-full items-center justify-center gap-2 rounded-xl bg-accent text-[17px] font-semibold text-white disabled:opacity-40"
              >
                {busy && <Spinner />}
                {title}
              </button>
              {phase.type === "found" && (
                <button
                  type="button"
                  onClick={changeServer}
                  className="flex h-11 w-full items-center justify-center text-[17px] text-accent-text"
                >
                  Use Another Server
                </button>
              )}
            </>
          )}
          <p id={noteID} className="px-4 text-[13px] text-secondary">
            {note}
          </p>
        </form>
        {notices.length > 0 && (
          <div className="mt-6 overflow-hidden rounded-xl bg-card">
            {notices.map((n) => (
              <ProblemBox
                key={n}
                tone="caution"
                title={unrevokedTitle}
                message={n}
              />
            ))}
          </div>
        )}
      </div>
    </main>
  )
}

/** A server too old for the app: what it runs, what the app needs, what it
 * cannot do yet, and who can fix it (the kit's ServerTooOldPresentation). */
function TooOld(props: {
  app: string
  error: ServerTooOld
  onTryAgain: () => void
  onChangeServer: () => void
}) {
  const { app, error } = props
  const host = new URL(error.server).host
  const { running, needed, missing } = error.verdict
  const release = (() => {
    const v = parseServerVersion(running)
    return !!v && isRelease(v)
  })()
  const runs =
    `${host} runs ${displayServerVersion(running)}` +
    (release ? "." : " that can't do it yet.")
  const summary = needed
    ? `${app} needs Substrate ${needed} or later. ${runs}`
    : `${app} needs a newer Substrate. ${runs}`
  return (
    <div className="overflow-hidden rounded-xl bg-card" role="alert">
      <div className="px-4 py-3">
        <h2 className="text-[17px] font-semibold">{host} needs an update</h2>
        <p className="mt-1 text-[15px]">{summary}</p>
        {missing.length > 0 && (
          <>
            <h3 className="mt-3 text-[13px] text-secondary">It can't yet</h3>
            <ul className="mt-1 list-disc pl-5 text-[15px]">
              {missing.map((m) => (
                <li key={m}>{m.charAt(0).toUpperCase() + m.slice(1)}</li>
              ))}
            </ul>
          </>
        )}
        <p className="mt-3 text-[13px] text-secondary">
          Whoever runs {host} can update it. Once it is, try again.
        </p>
      </div>
      <div className="grid grid-cols-2 border-t border-separator">
        <button
          type="button"
          onClick={props.onTryAgain}
          className="h-11 border-r border-separator text-[17px] text-accent-text"
        >
          Try Again
        </button>
        <button
          type="button"
          onClick={props.onChangeServer}
          className="h-11 text-[17px] text-accent-text"
        >
          Use Another Server
        </button>
      </div>
    </div>
  )
}
