/** A provider's sign-in details: the `oauth2`-trait client (a client ID and
 * a secret the owner creates with the provider) or a token provider's config
 * (a pasted key), edited through the ordinary record dialog, whose secret
 * inputs are write-only. It is step 2 of a provider's set-up, so it says what
 * to go and create, puts the redirect address the app must register beside
 * the fields, and orders the trait's two contracted properties ahead of a
 * bundle's own extras. It says which secrets are SET, read off the record (a
 * stored secret reads back as its redaction marker, never its value). The
 * help under each control is the console's; the declarations' developer
 * notes show in technical mode instead. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { OAuthCallbackNote } from "@/components/oauth-callback-note"
import { RecordConfigForm } from "@/components/record-config-form"
import { splitKind } from "@/lib/api/http"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { recordQueryOptions } from "@/lib/api/records"
import {
  OAUTH2_CLIENT_PROPERTIES,
  appHome,
  credentialHelp,
  type ProviderView,
} from "@/lib/sync"

export function CredentialsDialog({
  provider,
  providerName,
  open,
  onOpenChange,
}: {
  provider: ProviderView
  /** The provider's everyday name ("Google"). */
  providerName?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [technical] = useTechnicalDetails()
  const name = providerName ?? provider.name
  const kind = provider.configKind
  const help = useMemo(
    () =>
      kind && !technical
        ? credentialHelp(kind, name, provider.oauth)
        : undefined,
    [kind, technical, name, provider.oauth]
  )
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
  const declared = (kind.definition.properties ?? {}) as Record<
    string,
    { type?: string; displayName?: string }
  >
  const secrets = Object.entries(declared)
    .filter(([, p]) => p?.type === "secret")
    .map(([key, p]) => {
      const v = record?.properties?.[key]
      return {
        key,
        label: p.displayName || key,
        set: v !== undefined && v !== null && v !== "",
      }
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
      title={`${name} sign-in details`}
      first={provider.oauth ? OAUTH2_CLIENT_PROPERTIES : undefined}
      help={help}
      description={
        <span className="flex flex-col gap-1.5">
          <span>
            {provider.oauth
              ? `Create an app ${appHome(kind, name)}, register the redirect address below on it, then paste the app’s client ID and secret here. Every ${name} account you connect signs in through this one app.`
              : `Paste the token or key you created with ${name}. Every ${name} account you add uses it.`}
            {secrets.some((s) => s.set) &&
              " Leave a secret blank to keep the one you saved."}
          </span>
          {secrets.length > 0 && (
            <span className="flex flex-wrap gap-x-3 gap-y-1">
              {secrets.map((s) => (
                <span key={s.key}>
                  {s.label}:{" "}
                  <span className={s.set ? "text-ok" : "text-warning"}>
                    {s.set ? "saved" : "not saved yet"}
                  </span>
                </span>
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
