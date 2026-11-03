import type { AuthorizationPackagePublisher } from "@/lib/api/types"

export function PublisherDetails({
  publisher,
}: {
  publisher: AuthorizationPackagePublisher
}) {
  return (
    <dl className="grid gap-1 text-sm [overflow-wrap:anywhere]">
      <dt className="text-muted-foreground">Publisher</dt>
      <dd>
        {publisher.name
          ? `${publisher.name} · ${publisher.authority}`
          : publisher.authority}
      </dd>
      <dt className="text-muted-foreground">Authority document</dt>
      <dd>
        {publisher.fromFile ? (
          "from a file on this server"
        ) : (
          <a className="underline" href={publisher.documentUrl}>
            {publisher.documentUrl}
          </a>
        )}
      </dd>
    </dl>
  )
}
