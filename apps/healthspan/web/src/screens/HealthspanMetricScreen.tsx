import { Placeholder } from "../ui/Placeholder"
import { Screen } from "../ui/Screen"

export function HealthspanMetricScreen(props: { id: string }) {
  return (
    <Screen title="Healthspan metric">
      <Placeholder what={`Healthspan metric: ${props.id ?? "new"}`} />
    </Screen>
  )
}
