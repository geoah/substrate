import {
  AccountSection,
  LargeTitle,
  Row,
  Section,
  ToggleRow,
  useSubstrate,
} from "@substrate/react"
import { useSettings } from "./state"

/** Settings: the app's own sections beside the link's, as the kit's
 * SettingsPresenter puts them. */
export function SettingsScreen() {
  const [settings, set] = useSettings()
  const { manifest } = useSubstrate()
  return (
    <div className="mx-auto w-full max-w-2xl pb-32">
      <div className="h-[60px]" />
      <LargeTitle>Settings</LargeTitle>
      <div className="px-4">
        <Section
          header="Tasks"
          footer="Done and abandoned tasks show under each list's open ones. Kept on this device."
        >
          <ToggleRow
            label="Show Completed Tasks"
            checked={settings.showCompleted}
            onChange={(showCompleted) => set({ showCompleted })}
          />
        </Section>
        <AccountSection />
        <Section header="About">
          <Row label="App" value={manifest.name} />
          <Row label="Version" value={manifest.softwareVersion} />
        </Section>
      </div>
    </div>
  )
}
