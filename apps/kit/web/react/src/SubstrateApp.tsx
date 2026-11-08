import type { ReactNode } from "react"
import { ConnectScreen } from "./ConnectScreen"
import { useConnection } from "./context"
import { SubstrateProvider, type SubstrateProviderProps } from "./provider"
import { ToastHost } from "./toast"

export interface SubstrateAppProps extends SubstrateProviderProps {
  /** What the connect screen says the app is. */
  tagline?: string
  icon?: ReactNode
}

/** An app on the owner's substrate: the connect screen until a session is
 * kept, the sign-in's return handled, and the app with its records, outbox
 * and status once signed in (the kit's AppShell over the link). An app
 * with its own connect screen uses `SubstrateProvider`. */
export function SubstrateApp({
  tagline,
  icon,
  children,
  ...provider
}: SubstrateAppProps) {
  return (
    <ToastHost>
      <SubstrateProvider {...provider}>
        <Gate tagline={tagline} icon={icon}>
          {children}
        </Gate>
      </SubstrateProvider>
    </ToastHost>
  )
}

function Gate(props: {
  tagline?: string
  icon?: ReactNode
  children: ReactNode
}) {
  const { manifest, phase, problem, unrevoked, signIn } = useConnection()
  switch (phase) {
    case "signingIn":
      return <Waiting words="Signing in…" />
    case "signedOut":
      return (
        <ConnectScreen
          manifest={manifest}
          tagline={props.tagline}
          icon={props.icon}
          problem={problem}
          unrevoked={unrevoked}
          onSignIn={signIn}
        />
      )
    case "opening":
      return <Waiting words="Opening…" />
    case "open":
      return props.children
  }
}

function Waiting({ words }: { words: string }) {
  return (
    <div
      className="flex h-full items-center justify-center text-secondary"
      role="status"
    >
      <Spinner /> <span className="ml-3">{words}</span>
    </div>
  )
}

export function Spinner({ className = "" }: { className?: string }) {
  return (
    <span
      className={`inline-block size-5 rounded-full border-2 border-current border-t-transparent motion-safe:animate-spin ${className}`}
      aria-hidden
    />
  )
}
