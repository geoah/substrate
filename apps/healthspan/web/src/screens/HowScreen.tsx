import { useMemo } from "react"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import { explainer } from "../healthspan/explainer"
import { go } from "../route"
import { HowItWorks } from "../ui/healthspan/HowItWorks"
import { Screen } from "../ui/Screen"

/** How the age score works. Drawn at once from the engine's own numbers;
 * the targets and the sources fill in once the owner and the reads have
 * answered. */
export function HowScreen() {
  const { today } = useMode()
  const read = useBackend().healthspan.useHealthspan()
  const view = read.data
  const e = useMemo(
    () =>
      explainer(
        view?.report?.snapshots.at(-1),
        view?.profile ?? {},
        view?.series,
        view?.today ?? today
      ),
    [view, today]
  )
  return (
    <Screen
      title="How the age score works"
      back={{
        label: "Healthspan",
        onClick: () => go({ screen: "healthspan", query: "" }),
      }}
    >
      <HowItWorks explainer={e} />
    </Screen>
  )
}
