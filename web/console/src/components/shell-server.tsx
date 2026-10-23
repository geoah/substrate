import { changeServer, nativeShell } from "@/lib/shell"

/** The substrate a native app is pointed at, on the sign-in and register
 * doors, with the way to point it elsewhere. A browser already names its
 * substrate in the address bar, so outside the shell there is nothing here. */
export function ShellServer() {
  const shell = nativeShell()
  if (!shell) return null
  return (
    <p className="text-center text-xs text-muted-foreground">
      <span>Server</span>{" "}
      <span className="font-mono [overflow-wrap:anywhere] text-foreground">
        {shell.server}
      </span>{" "}
      <button
        type="button"
        onClick={() => changeServer()}
        className="underline underline-offset-4 hover:text-foreground"
      >
        Change
      </button>
    </p>
  )
}
