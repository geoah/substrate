import { LargeTitle, NavBar, SubstrateSettings } from "@substrate/react"
import { Item } from "./kinds"

/** Settings: the template has no settings of its own, so the screen is the
 * kit's sections, in every app's order. An app's own go above them. */
export function SettingsScreen(props: { onBack: () => void }) {
  return (
    <div className="mx-auto w-full max-w-2xl pb-16">
      <NavBar onBack={props.onBack} backLabel="Items" />
      <LargeTitle>Settings</LargeTitle>
      <div className="px-4">
        <SubstrateSettings kindLabels={{ [Item.kind]: "Item" }} />
      </div>
    </div>
  )
}
