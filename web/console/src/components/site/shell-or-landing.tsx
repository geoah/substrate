import { AppShell } from "@/components/app-shell"
import { LandingPage } from "@/components/site/landing-page"
import { hasSession } from "@/lib/api/session"
import { peekSite } from "@/lib/site"

/** The shell, or the landing page for the signed-out reader the shell's gate
 * let through at `/`. Nothing under the shell mounts in the second case, so no
 * page asks the API for anything without a token. */
export function ShellOrLanding() {
  const site = peekSite()
  if (!hasSession() && site?.landing) return <LandingPage site={site} />
  return <AppShell />
}
