/** What the technical view shows under a `call:` scope's sentence (decision
 * 0219): the functions it matches today, each by name and full id, with the
 * kinds it and what it calls read and write, and whether the request's
 * other scopes cover that reach, since the server refuses a call they do
 * not. */

import type { AuthorizationCallFunction } from "@/lib/api/types"
import { functionName } from "@/lib/app-scopes"

function ReachList({ label, kinds }: { label: string; kinds: string[] }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd>
        {kinds.length === 0 ? (
          "Nothing"
        ) : (
          <ul className="flex flex-col">
            {kinds.map((k) => (
              <li
                key={k}
                className="font-mono text-xs [overflow-wrap:anywhere]"
              >
                {k}
              </li>
            ))}
          </ul>
        )}
      </dd>
    </>
  )
}

export function FunctionList({
  functions,
}: {
  functions: AuthorizationCallFunction[]
}) {
  if (functions.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        Nothing yet: no function in your repository matches.
      </p>
    )
  }
  return (
    <ul className="flex w-full flex-col gap-2">
      {functions.map((f) => (
        <li key={f.identity} className="flex flex-col gap-0.5">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="text-[13px]">{functionName(f.identity)}</span>
            <span className="min-w-0 font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
              {f.identity}
            </span>
          </div>
          {f.description && (
            <p className="text-xs text-muted-foreground">{f.description}</p>
          )}
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
            <ReachList label="Reads" kinds={f.reads} />
            <ReachList label="Writes" kinds={f.writes} />
          </dl>
          {f.covered ? (
            <p className="text-xs text-muted-foreground">
              The scopes above cover what it reaches, so the app may run it.
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">
              The app can’t run it until its scopes also cover{" "}
              {(f.uncovered ?? []).map((u, i) => (
                <span key={u}>
                  {i > 0 && ", "}
                  <span className="font-mono [overflow-wrap:anywhere] text-foreground">
                    {u}
                  </span>
                </span>
              ))}
              .
            </p>
          )}
        </li>
      ))}
    </ul>
  )
}
