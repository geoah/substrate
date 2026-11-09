import { describe } from "@substrate/client"
import { ProblemBox } from "@substrate/react"

/** Why a lab read failed, in the server's words, with Try Again. */
export function ReadProblem(props: { error: unknown; onRetry: () => void }) {
  return (
    <div className="overflow-hidden rounded-[10px] bg-card">
      <ProblemBox
        title="The lab results could not be read"
        message={describe(props.error)}
        action={{ title: "Try Again", onClick: props.onRetry }}
      />
    </div>
  )
}
