/** The owner's own contact, as Settings shows it under Account: the contact
 * the core setting names (decision record 0231), linked, or the way to give
 * a name where it names none. Both go to Settings, Your name. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { EmptyValue } from "@/components/identity/empty-value"
import { Facts } from "@/components/identity/facts"
import { RecordRef } from "@/components/identity/record-ref"
import { SettingRow } from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
import { ownerContactQueryOptions } from "@/lib/api/owner-contact"
import { CONTACT_KIND } from "@/lib/core-kinds"
import { recordTitle } from "@/lib/format"

export function OwnerContactRow() {
  const owner = useQuery(ownerContactQueryOptions)
  const contact = owner.data?.contact
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
      <Facts
        rows={[
          [
            "Your contact",
            owner.isPending ? (
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
            ),
          ],
        ]}
      />
    </SettingRow>
  )
}
