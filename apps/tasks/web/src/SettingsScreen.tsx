import {
  LargeTitle,
  Row,
  Section,
  SelectRow,
  SubstrateSettings,
  ToggleRow,
  useSubstrate,
} from "@substrate/react"
import { CircleAlert } from "lucide-react"
import { hourWords, type Order } from "./model"
import type { Permission } from "./reminders"
import { kindLabel } from "./sources"
import { openSettings, reminderHours, useSettings, useTasks } from "./state"
import { words } from "./words"

/** What this browser is called where TasksSettings names the device. */
const device = "This Browser"

/** Settings: the app's reminders and lists (TasksSettings.sections), the
 * keys a keyboard has, then the link's sections the kit draws. */
export function SettingsScreen(props: {
  permission: Permission
  planned: number
  ask: () => Promise<void>
  keyboard: boolean
}) {
  const [settings, set] = useSettings()
  const { manifest } = useSubstrate()
  const model = useTasks()
  const { permission } = props
  const name = manifest.name

  return (
    <div className="mx-auto w-full max-w-2xl pb-32">
      <div className="h-[60px]" />
      <LargeTitle>Settings</LargeTitle>
      <div className="px-4">
        <Section>
          <Row
            label={words.settings.collections}
            value={words.settings.on(model.sources.enabled.length)}
            chevron
            onClick={() => openSettings({ page: "collections" })}
          />
        </Section>
        <Section
          header="Reminders"
          footer={
            <>
              {settings.remind
                ? "A task due at a time reminds you then; a task due on a day, at the hour above."
                : `Turned on, you are asked once whether ${name} may send notifications.`}{" "}
              Reminders show only while {name} is open in this browser.
            </>
          }
        >
          <ToggleRow
            label="Remind Me When Tasks Are Due"
            checked={settings.remind}
            onChange={(remind) => {
              set({ remind })
              // Asked when the owner turns reminders on, never before.
              if (remind) void props.ask()
            }}
          />
          {settings.remind && (
            <>
              <SelectRow
                label={
                  <span className="block">
                    <span className="block">Tasks Due on a Day</span>
                    <span className="block text-[13px] text-secondary">
                      When the reminder comes
                    </span>
                  </span>
                }
                value={String(settings.remindHour)}
                options={reminderHours.map((h) => ({
                  value: String(h),
                  label: hourWords(h),
                }))}
                onChange={(h) => set({ remindHour: Number(h) })}
              />
              {permission === "off" || permission === "unsupported" ? (
                <div className="flex gap-3 px-4 py-2.5">
                  <CircleAlert className="mt-0.5 size-5 shrink-0 text-warn" />
                  <div className="min-w-0">
                    <p className="text-[17px]">
                      {permission === "off"
                        ? `Notifications are off for ${name}`
                        : "This browser cannot show notifications"}
                    </p>
                    <p className="text-[13px] text-secondary">
                      {permission === "off"
                        ? "Turn them on in your browser's settings for this site to be reminded."
                        : "Open Substrate Tasks in a browser that can, or add it to your Home Screen, to be reminded."}
                    </p>
                  </div>
                </div>
              ) : (
                <Row
                  label={`Reminders Set on ${device}`}
                  value={String(props.planned)}
                />
              )}
            </>
          )}
        </Section>
        <Section header="Lists">
          <SelectRow<Order>
            label="Sort Tasks By"
            value={settings.order}
            options={[
              { value: "manual", label: "My Order" },
              { value: "due", label: "Due Date" },
            ]}
            onChange={(order) => set({ order })}
          />
          <ToggleRow
            label="Show Completed Tasks"
            checked={settings.showCompleted}
            onChange={(showCompleted) => set({ showCompleted })}
          />
        </Section>
        {props.keyboard && (
          <Section
            header="Keyboard"
            footer="Where the focus is not in a field."
          >
            {shortcuts.map(([keys, what]) => (
              <Row
                key={what}
                label={what}
                value={<kbd className="font-sans">{keys}</kbd>}
              />
            ))}
          </Section>
        )}
        <SubstrateSettings
          kindLabels={Object.fromEntries(
            [
              ...model.sources.collections.map((c) => c.kind),
              ...[...model.groups.keys()].map((p) =>
                p.split("/").slice(0, 3).join("/")
              ),
            ].map((k) => [k, kindLabel(k, model.declaration(k)).singular])
          )}
        />
      </div>
    </div>
  )
}

const mac =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)

/** The Mac app's menu commands, and what its lists answer to. */
export const shortcuts: [string, string][] = [
  ["N", "New Task"],
  [mac ? "⌘ ," : "Ctrl ,", "Settings"],
  ["↑ ↓", "Previous or Next Task"],
  ["E", "Edit Task"],
  ["A", "Accept a Suggestion"],
  ["D", "Dismiss a Suggestion"],
  ["S", "Select Suggestions"],
  ["P", "Pin or Unpin a Group"],
  ["Z", "Undo a Decision"],
  [mac ? "⌥ ↑ ↓" : "Alt ↑ ↓", "Move a Task Up or Down"],
  [mac ? "⌘ ↵" : "Ctrl ↵", "Save in the Editor"],
  ["Esc", "Close"],
]
