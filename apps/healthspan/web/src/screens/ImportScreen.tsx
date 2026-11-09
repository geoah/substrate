import { Placeholder } from "../ui/Placeholder"
import { Screen } from "../ui/Screen"

export function ImportScreen(props: { id?: string }) {
  return (
    <Screen title="Import">
      <Placeholder what={`Import: ${props.id ?? "new"}`} />
    </Screen>
  )
}
