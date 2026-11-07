/** "Unverified" beside the signed-in repository's name until the repository
 * proves it controls the name's DNS (Settings, Verify domain; decision 0208).
 * Registration takes a name on its owner's word, so on a shared server the
 * name alone says nothing about who holds the domain. Renders nothing while
 * the repository record is loading or a name under the server's own domain
 * is being checked, and nothing once the name is verified. */

import { Pill } from "@/components/identity/pill"
import { useRepository } from "@/hooks/use-repository"

export function UnverifiedMark({ className }: { className?: string }) {
  const { repository, checking } = useRepository()
  if (
    checking ||
    !repository.data?.authority ||
    repository.data.domainVerifiedAt
  ) {
    return null
  }
  return (
    <Pill
      tone="warn"
      className={className}
      title="Nothing has proved this repository controls the name’s DNS yet. Its owner verifies it under Settings, Verify domain."
    >
      Unverified
    </Pill>
  )
}
