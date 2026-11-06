/** Settings, Your name (`/settings/name`): the name step registration asks,
 * for a repository registered before it did, and for renaming later. Where
 * the setting names a live contact, the page shows it and saving renames it;
 * otherwise saving creates the owner's contact and the setting naming it. */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"

import { Facts } from "@/components/identity/facts"
import { PageHeader } from "@/components/identity/page-header"
import { DocPage } from "@/components/identity/page-layout"
import { RecordRef } from "@/components/identity/record-ref"
import { OwnerNameForm } from "@/components/owner-name-form"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "@/components/ui/toast"
import { ownerContactQueryOptions } from "@/lib/api/owner-contact"
import { getRepository } from "@/lib/api/session"
import { CONTACT_KIND } from "@/lib/core-kinds"
import { recordTitle } from "@/lib/format"

export function OwnerNamePage() {
  const owner = useQuery(ownerContactQueryOptions)
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const contact = owner.data?.contact

  async function saved() {
    await queryClient.invalidateQueries({
      queryKey: ownerContactQueryOptions.queryKey,
    })
    toast.add({ type: "success", title: "Your name is saved." })
    await navigate({ to: "/settings" })
  }

  return (
    <DocPage className="pb-20">
      <PageHeader
        title="Your name"
        description="Your name is saved as your own contact, so your agents and apps know which contact is you."
      />
      <div className="mt-6 flex max-w-xl flex-col gap-6">
        {owner.isPending ? (
          <Skeleton className="h-28 max-w-sm" />
        ) : owner.isError ? (
          <p
            role="alert"
            className="rounded-lg bg-bad-soft px-3 py-2 text-sm text-destructive"
          >
            Your contact didn’t load: {owner.error.message}
          </p>
        ) : (
          <>
            {contact && (
              <Facts
                rows={[
                  [
                    "Your contact",
                    <RecordRef
                      kind={CONTACT_KIND}
                      id={contact.id}
                      title={recordTitle(contact.properties) || undefined}
                    />,
                  ],
                ]}
              />
            )}
            <OwnerNameForm
              repository={getRepository() ?? ""}
              current={owner.data}
              layout="page"
              submitLabel="Save"
              secondaryLabel="Cancel"
              onSaved={saved}
              onSecondary={() => void navigate({ to: "/settings" })}
            />
          </>
        )}
      </div>
    </DocPage>
  )
}
