import { AccessBanner, RefusedBanner } from "@substrate/react"
import { selectSection } from "./state"

/** What stands in the way above a column (the native app's `Notices`):
 * the kit's access banner, and the changes the server refused, which open
 * Settings, where their Retry and Discard are. */
export function Notices(props: { wide: boolean }) {
  return (
    <>
      <AccessBanner className="mx-4 mb-3" />
      <RefusedBanner
        className="mx-4 mb-3"
        onOpenSettings={() => selectSection("settings", props.wide)}
      />
    </>
  )
}
