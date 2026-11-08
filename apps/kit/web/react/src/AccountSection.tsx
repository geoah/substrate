import { ApprovedAccessSection, RepositorySection } from "./settings"

/** @deprecated Use `SubstrateSettings`, or `RepositorySection` and
 * `ApprovedAccessSection` where an app places them itself. */
export function AccountSection() {
  return (
    <>
      <RepositorySection />
      <ApprovedAccessSection />
    </>
  )
}
