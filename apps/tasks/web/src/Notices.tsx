import { AccessBanner, ProblemBox, useParkedWrites } from "@substrate/react"
import { selectSection } from "./state"

/** What stands in the way above a column (the native app's `Notices`):
 * the kit's access banner, and the changes the server refused, which open
 * Settings, where their Retry and Discard are. */
export function Notices(props: { wide: boolean }) {
  const parked = useParkedWrites()
  return (
    <>
      <AccessBanner className="mx-4 mb-3" />
      {parked.length > 0 && (
        <div className="mx-4 mb-3 overflow-hidden rounded-[10px] bg-card">
          <ProblemBox
            tone="caution"
            title={
              parked.length === 1
                ? "1 change needs you"
                : `${parked.length} changes need you`
            }
            message="Your substrate refused it. Retry or discard it in Settings."
            action={{
              title: "Open Settings",
              onClick: () => selectSection("settings", props.wide),
            }}
          />
        </div>
      )}
    </>
  )
}
