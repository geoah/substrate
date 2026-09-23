/** A provider's credentials: the `oauth2`-trait client (a client id and a
 * secret the owner creates with the provider) or a token provider's config
 * (a pasted key), edited through the ordinary record dialog, whose secret
 * inputs are write-only. It is the first of the three steps a first-time
 * user takes on the Connections page, so it says what to go and create, puts
 * the callback URL the client must register beside the fields, and orders
 * the trait's two contracted properties ahead of a bundle's own extras. The
 * header says which secrets are SET, read off the record (a stored secret
 * reads back as its redaction marker, never its value). */

import { useQuery } from "@tanstack/react-query"

import { OAuthCallbackNote } from "@/components/oauth-callback-note"
import { RecordConfigForm } from "@/components/record-config-form"
import { Badge } from "@/components/ui/badge"
import { splitKind } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import { OAUTH2_CLIENT_PROPERTIES, type ProviderView } from "@/lib/sync"

export function CredentialsDialog({
  provider,
  open,
  onOpenChange,
}: {
  provider: ProviderView
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const kind = provider.configKind
  const parts = kind ? splitKind(kind.identity) : undefined
  const existing = useQuery({
    ...recordQueryOptions(
      parts?.authority ?? "",
      parts?.pkg ?? "",
      kind?.name ?? "",
      provider.configRecord ?? ""
    ),
    enabled: Boolean(kind && provider.configRecord && open),
  })
  if (!kind) return null
  const record = provider.configRecord ? existing.data : undefined
  const secrets = Object.entries(
    (kind.definition.properties ?? {}) as Record<string, { type?: string }>
  )
    .filter(([, p]) => p?.type === "secret")
    .map(([name]) => name)
  const setState = secrets.map((name) => {
    const v = record?.properties?.[name]
    return { name, set: v !== undefined && v !== null && v !== "" }
  })
  if (provider.configRecord && existing.isPending) {
    return null
  }
  return (
    <RecordConfigForm
      type={kind}
      record={record}
      open={open}
      onOpenChange={onOpenChange}
      title={
        record
          ? `Edit ${provider.name} credentials`
          : `Set up ${provider.name} credentials`
      }
      first={provider.oauth ? OAUTH2_CLIENT_PROPERTIES : undefined}
      description={
        <span className="flex flex-col gap-1">
          <span>
            {provider.oauth
              ? `Create an OAuth client with ${provider.name}, register the callback URL below as its redirect URI, then paste the client's ID and secret here. Every account you add to ${provider.name} connects through this one client.`
              : `Paste the token or key you created with ${provider.name}. Every account you add to ${provider.name} uses it.`}{" "}
            A secret is write-only: it never reads back, and a blank one keeps
            what is stored.
          </span>
          {setState.length > 0 && (
            <span className="flex flex-wrap gap-1.5 pt-1">
              {setState.map((s) => (
                <Badge
                  key={s.name}
                  variant="outline"
                  className={
                    s.set
                      ? "gap-1 font-normal"
                      : "gap-1 font-normal text-warning"
                  }
                >
                  <span className="data">{s.name}</span>
                  <span>{s.set ? "· set" : "· not set"}</span>
                </Badge>
              ))}
            </span>
          )}
        </span>
      }
      lead={
        provider.oauth ? (
          <OAuthCallbackNote providerId={provider.id} />
        ) : undefined
      }
    />
  )
}
