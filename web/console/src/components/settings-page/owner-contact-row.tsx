/** The owner's own contact, as Settings shows it under Account: the contact
 * the core setting names (decision record 0231), linked, or the way to give
 * a name where it names none. The name goes to Settings, Your name; which
 * contact is the owner's is chosen here, from the contacts already held. */

import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { EmptyValue } from "@/components/identity/empty-value"
import { RecordRef } from "@/components/identity/record-ref"
import { RecordCombobox } from "@/components/record/record-combobox"
import { SettingRow } from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  ownerContactQueryOptions,
  saveOwnerContactSetting,
} from "@/lib/api/owner-contact"
import { CONTACT_KIND } from "@/lib/core-kinds"
import { recordTitle } from "@/lib/format"

export function OwnerContactRow() {
  const queryClient = useQueryClient()
  const owner = useQuery(ownerContactQueryOptions)
  const kinds = useQuery(kindsQueryOptions)
  const [choosing, setChoosing] = useState(false)
  const contact = owner.data?.contact
  const choose = useMutation({
    mutationFn: (contactId: string) =>
      saveOwnerContactSetting({
        contactId,
        exists: Boolean(owner.data?.setting),
      }),
    onSuccess: async () => {
      setChoosing(false)
      await queryClient.invalidateQueries({
        queryKey: ownerContactQueryOptions.queryKey,
      })
    },
  })
  return (
    <SettingRow
      title="Your name"
      description="Saved as your own contact, so your agents and apps know which contact is you."
      control={
        owner.isSuccess && (
          <Button
            variant="outline"
            size="sm"
            nativeButton={false}
            render={<Link to="/settings/name" />}
          >
            {contact ? "Change name" : "Tell us your name"}
          </Button>
        )
      }
    >
      <div className="flex flex-col gap-1 border-t pt-2.5 text-[13.5px]">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
          <span className="text-muted-foreground">Your contact</span>
          {owner.isSuccess && !choosing && (
            <Button variant="ghost" size="sm" onClick={() => setChoosing(true)}>
              {contact ? "Choose another" : "Choose a contact"}
            </Button>
          )}
        </div>
        {choosing ? (
          <div className="flex flex-wrap items-center gap-2">
            <div className="min-w-0 flex-1">
              <RecordCombobox
                pin={CONTACT_KIND}
                kinds={kinds.data ?? []}
                value={contact?.id}
                valueTitle={
                  contact
                    ? recordTitle(contact.properties) || undefined
                    : undefined
                }
                ariaLabel="Your contact"
                placeholder="Choose a contact"
                defaultOpen
                onSelect={(id) => {
                  if (id && id !== contact?.id) choose.mutate(id)
                  else setChoosing(false)
                }}
              />
            </div>
            <Button
              variant="ghost"
              size="sm"
              disabled={choose.isPending}
              onClick={() => setChoosing(false)}
            >
              Cancel
            </Button>
          </div>
        ) : owner.isPending ? (
          <span className="text-faint">Reading…</span>
        ) : owner.isError ? (
          <EmptyValue>
            Your contact didn’t load: {owner.error.message}
          </EmptyValue>
        ) : contact ? (
          <RecordRef
            kind={CONTACT_KIND}
            id={contact.id}
            title={recordTitle(contact.properties) || undefined}
          />
        ) : (
          <EmptyValue>None yet</EmptyValue>
        )}
        {choose.isError && (
          <p role="alert" className="text-[12.5px] text-destructive">
            Your contact didn’t change: {choose.error.message}
          </p>
        )}
        <p className="text-[12.5px] text-faint">
          The card in your contacts that is you. Your agents and apps use it
          when they need to know which person is you. If you already have a card
          for yourself, choose it here.
        </p>
      </div>
    </SettingRow>
  )
}
