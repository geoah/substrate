/** The app named the repository it wants (`login_hint`, decision 0208) and
 * the console is signed in to another: approving here would connect the app
 * to the repository signed in, which the app then refuses and revokes. The
 * request belongs to the repository that read it first, so the way to the
 * one the app asked for is to sign out and start again from the app. */

import { useState } from "react"
import { useNavigate } from "@tanstack/react-router"

import { Warning } from "@/components/authorize/parts"
import { Button } from "@/components/ui/button"
import { logout } from "@/lib/api/auth"

export function HintMismatch({
  hint,
  repository,
}: {
  /** The repository the app asked for. */
  hint: string
  /** The repository the console is signed in to. */
  repository: string
}) {
  const navigate = useNavigate()
  const [leaving, setLeaving] = useState(false)
  async function signOut() {
    setLeaving(true)
    await logout()
    void navigate({ to: "/login", search: { login_hint: hint }, replace: true })
  }
  return (
    <Warning title="The app asked for another repository">
      <p>
        The app asked to connect to{" "}
        <span className="data [overflow-wrap:anywhere]">{hint}</span>, and you
        are signed in to{" "}
        <span className="data [overflow-wrap:anywhere]">{repository}</span>.
        Approving here connects it to {repository}. To connect it to {hint},
        sign out, sign in to {hint} and start again from the app.
      </p>
      <div>
        <Button
          variant="outline"
          size="sm"
          disabled={leaving}
          onClick={() => void signOut()}
        >
          Sign out
        </Button>
      </div>
    </Warning>
  )
}
