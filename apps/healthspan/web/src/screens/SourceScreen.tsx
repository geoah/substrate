import { Placeholder } from "../ui/Placeholder"
import { Screen } from "../ui/Screen"

export function SourceScreen(props: { id: string }) {
  return (
    <Screen title="Source">
      <Placeholder what={`Source: ${props.id ?? "new"}`} />
    </Screen>
  )
}
