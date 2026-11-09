import { Placeholder } from "../ui/Placeholder"
import { Screen } from "../ui/Screen"

export function LabScreen(props: { entry: string; query: string }) {
  return (
    <Screen title="Lab metric">
      <Placeholder what={`Lab metric: ${props.entry ?? "new"}`} />
    </Screen>
  )
}
