import type { AppManifest } from "@substrate/auth"
import { describe, locate, type LocatedServer } from "@substrate/client"
import { useState, type FormEvent, type ReactNode } from "react"
import { Spinner } from "./SubstrateApp"

const lastServerKey = "substrate.lastServer"

/** The connect screen: the owner's server or repository name, checked
 * against its discovery documents, then the server's own sign-in and
 * approval page (the kit's SignIn screen). */
export function ConnectScreen(props: {
  manifest: AppManifest
  tagline?: string
  icon?: ReactNode
  problem?: string | null
  onSignIn: (located: LocatedServer) => Promise<void>
}) {
  const [typed, setTyped] = useState(() => {
    try {
      return localStorage.getItem(lastServerKey) ?? ""
    } catch {
      return ""
    }
  })
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(props.problem ?? null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setProblem(null)
    try {
      const located = await locate(typed)
      try {
        localStorage.setItem(lastServerKey, typed.trim())
      } catch {
        // a private window keeps nothing
      }
      await props.onSignIn(located)
    } catch (err) {
      setProblem(describe(err))
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center bg-grouped px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          {props.icon && <div className="mb-4">{props.icon}</div>}
          <h1 className="text-[28px] font-bold tracking-tight">
            {props.manifest.name}
          </h1>
          {props.tagline && (
            <p className="mt-2 text-[15px] text-secondary">{props.tagline}</p>
          )}
        </div>
        <form onSubmit={submit} className="space-y-4">
          <div className="overflow-hidden rounded-xl bg-card">
            <label
              className="block px-4 pt-3 text-[13px] text-secondary"
              htmlFor="sx-server"
            >
              Your server or repository
            </label>
            <input
              id="sx-server"
              className="w-full bg-transparent px-4 pt-1 pb-3 text-[17px] outline-none placeholder:text-tertiary"
              placeholder="ada.example.com"
              autoComplete="url"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              inputMode="url"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoFocus
            />
          </div>
          <p className="px-4 text-[13px] text-secondary">
            Type your repository's name or your Substrate server's address. You
            sign in on your server's own page and approve what{" "}
            {props.manifest.name} may do; the app never sees your password.
          </p>
          {problem && (
            <p
              role="alert"
              className="rounded-xl bg-card px-4 py-3 text-[15px] text-danger"
            >
              {problem}
            </p>
          )}
          <button
            type="submit"
            disabled={busy || !typed.trim()}
            className="flex h-12 w-full items-center justify-center rounded-xl bg-accent text-[17px] font-semibold text-white disabled:opacity-40"
          >
            {busy ? <Spinner /> : "Sign In"}
          </button>
        </form>
      </div>
    </div>
  )
}
