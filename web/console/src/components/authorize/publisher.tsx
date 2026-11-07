/** Who publishes the app asking (decision 0209). A verified app's publisher
 * is confirmed by its authority's `_substrate` record and the authority
 * document it names, and its access goes to that authority. Anything else
 * is unverified: the page shows the name the app claims, marked so, where
 * the access goes, and which claim could not be confirmed. */

import { BadgeCheckIcon } from "lucide-react"

import { Fact, Warning } from "@/components/authorize/parts"
import type { AuthorizationRequest } from "@/lib/api/types"
import { accessGoesTo } from "@/lib/app-publisher"

/** The lines under the app's name that say who publishes it. A verified
 * publisher is its name and the authority that proved it, on one line: the
 * name is the publisher's own word, so the authority stays beside it. */
export function PublisherLines({ request }: { request: AuthorizationRequest }) {
  const { app } = request
  const publisher = app.publisher
  if (app.verified) {
    const named =
      publisher.name &&
      publisher.name.toLowerCase() !== publisher.authority?.toLowerCase()
    return (
      <>
        <p className="flex flex-wrap items-center gap-x-1 text-sm">
          <BadgeCheckIcon aria-hidden className="size-4 text-primary" />
          <span>
            by{" "}
            <span className="font-medium [overflow-wrap:anywhere]">
              {named ? publisher.name : publisher.authority}
            </span>
          </span>
          {named && (
            <span className="[overflow-wrap:anywhere]">
              {" "}
              · {publisher.authority}
            </span>
          )}
        </p>
        {publisher.fromFile && (
          <Warning title="Verified by a file on this server, not by DNS">
            This server is configured to read {publisher.authority}’s authority
            document from a local file in place of its DNS record. Whoever set
            up the server vouches for this app, not {publisher.authority}.
          </Warning>
        )}
      </>
    )
  }
  return (
    <>
      {app.name && (
        <p className="text-xs text-muted-foreground">
          It calls itself{" "}
          <span className="[overflow-wrap:anywhere] text-foreground">
            {app.name}
          </span>
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        <span className="font-medium text-foreground">Unverified.</span> Its
        access goes to{" "}
        <span className="[overflow-wrap:anywhere] text-foreground">
          {accessGoesTo(request)}
        </span>
      </p>
      {publisher.unconfirmed && (
        <p className="text-xs [overflow-wrap:anywhere] text-muted-foreground">
          {publisher.authority
            ? `It says ${publisher.authority} publishes it, which could not be confirmed: `
            : "Could not be confirmed: "}
          {publisher.unconfirmed}.
        </p>
      )}
    </>
  )
}

/** The technical view of the publisher check: the record read, the document
 * it names and whether that document lists the client id. */
export function PublisherFacts({ request }: { request: AuthorizationRequest }) {
  const { publisher } = request.app
  if (!publisher.authority && !publisher.record && !publisher.documentUrl)
    return null
  return (
    <Fact label="Publisher check">
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1">
        {publisher.authority && (
          <>
            <dt className="text-muted-foreground">Authority</dt>
            <dd className="font-mono text-xs leading-5 [overflow-wrap:anywhere]">
              {publisher.authority}
            </dd>
          </>
        )}
        <dt className="text-muted-foreground">Record</dt>
        <dd className="font-mono text-xs leading-5 [overflow-wrap:anywhere]">
          {publisher.record || "none read"}
        </dd>
        <dt className="text-muted-foreground">Document</dt>
        <dd className="font-mono text-xs leading-5 [overflow-wrap:anywhere]">
          {publisher.documentUrl || "none read"}
        </dd>
        <dt className="text-muted-foreground">Read from</dt>
        <dd>
          {publisher.fromFile
            ? "A local file (SUBSTRATE_INSECURE_AUTHORITY_DOCS)"
            : "DNS and the document it names"}
        </dd>
        <dt className="text-muted-foreground">Lists this client id</dt>
        <dd>{publisher.listed ? "Yes" : "No"}</dd>
      </dl>
    </Fact>
  )
}
