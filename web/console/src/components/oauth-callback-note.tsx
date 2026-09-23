/** The OAuth callback URL, read-only with a copy affordance: the redirect URI
 * the owner registers on the client they create with a provider. It is the
 * SUBSTRATE's origin, baked at build time (`lib/api/catalog.ts`), never the
 * console's, because the provider matches the redirect URI exactly. Shown in
 * the two places a person is about to paste a client id: a provider bundle's
 * Setup surface and the Connections page's credentials dialog. */

import { useState } from "react"
import { ArrowUpRightIcon, CheckIcon, CopyIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { oauthCallbackURL } from "@/lib/api/catalog"

/** Where a provider documents creating an OAuth client, keyed by the bundle
 * id's package word. A provider not listed gets the URL and no link. */
const PROVIDER_OAUTH_DOCS: Record<string, { href: string; label: string }> = {
  google: {
    href: "https://support.google.com/cloud/answer/6158849",
    label: "Google's guide",
  },
  github: {
    href: "https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app",
    label: "GitHub's guide",
  },
}

export function OAuthCallbackNote({ providerId }: { providerId?: string }) {
  const url = oauthCallbackURL()
  const [copied, setCopied] = useState(false)
  const docs = providerId
    ? PROVIDER_OAUTH_DOCS[providerId.slice(providerId.lastIndexOf("/") + 1)]
    : undefined
  return (
    <div className="rounded-md border bg-muted/30 px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">OAuth callback URL</span>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 gap-1 px-2 text-xs"
          onClick={() => {
            void navigator.clipboard?.writeText(url)
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          }}
        >
          {copied ? (
            <CheckIcon className="size-3" />
          ) : (
            <CopyIcon className="size-3" />
          )}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <p className="mt-1 data text-xs break-all text-muted-foreground">{url}</p>
      <p className="mt-1.5 text-xs text-muted-foreground">
        Register this as the redirect URI of the OAuth client you create with
        the provider.
        {docs && (
          <>
            {" "}
            <a
              href={docs.href}
              target="_blank"
              rel="noreferrer"
              className="underline-offset-4 hover:underline"
            >
              {docs.label}
              <ArrowUpRightIcon className="inline size-3 align-text-top" />
            </a>
          </>
        )}
      </p>
    </div>
  )
}
