import { Placeholder } from "../ui/Placeholder"
import { Screen } from "../ui/Screen"

export function ReportScreen(props: { id: string }) {
  return (
    <Screen title="Report">
      <Placeholder what={`Report: ${props.id ?? "new"}`} />
    </Screen>
  )
}
