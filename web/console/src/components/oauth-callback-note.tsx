/** The OAuth callback URL, read-only with a copy button: the redirect
 * address a person registers on the app they create with a provider. It is
 * the SUBSTRATE's origin, baked at build time (`lib/api/catalog.ts`), never
 * the console's, because the provider matches the redirect exactly. Shown
 * where a person is about to paste a client ID: the provider page's
 * sign-in step and the sign-in details dialog. */

import { ArrowUpRightIcon } from "lucide-react"

import { CopyButton } from "@/components/identity/copy-button"
import { oauthCallbackURL } from "@/lib/api/catalog"
import { cn } from "@/lib/utils"

/** Where a provider documents creating an OAuth app, keyed by the bundle
 * id's package word. A provider not listed gets the address and no link. */
const PROVIDER_OAUTH_DOCS: Record<string, { href: string; label: string }> = {
  google: {
    href: "https://support.google.com/cloud/answer/6158849",
    label: "Google’s guide",
  },
  github: {
    href: "https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app",
    label: "GitHub’s guide",
  },
}

export function OAuthCallbackNote({
  providerId,
  className,
}: {
  providerId?: string
  className?: string
}) {
  const url = oauthCallbackURL()
  const docs = providerId
    ? PROVIDER_OAUTH_DOCS[providerId.slice(providerId.lastIndexOf("/") + 1)]
    : undefined
  return (
    <div
      data-slot="oauth-callback"
      className={cn(
        "rounded-lg border bg-panel px-3 py-2.5 text-[12.5px]",
        className
      )}
    >
      <div className="font-medium text-foreground">
        Redirect address to register
      </div>
      <div className="mt-1 flex items-center gap-1">
        <span className="min-w-0 font-mono text-xs [overflow-wrap:anywhere]">
          {url}
        </span>
        <CopyButton value={url} label="Copy the redirect address" />
      </div>
      <p className="mt-1 text-muted-foreground">
        Paste it as the redirect URI of the app you create with the provider.
        {docs && (
          <>
            {" "}
            <a
              href={docs.href}
              target="_blank"
              rel="noreferrer"
              className="text-primary-text underline-offset-2 hover:underline"
            >
              {docs.label}
              <ArrowUpRightIcon
                aria-hidden
                className="inline size-3 align-text-top"
              />
            </a>
          </>
        )}
      </p>
    </div>
  )
}
