import { Alert } from "@substrate/react"
import { choosingStore, useTaskActions } from "./actions"
import { valueLabel, type ActionName } from "./sources"
import { useStore, useTasks } from "./state"
import { words } from "./words"

/** What the sheet over several values is titled: the contract's two, and
 * the same shape for the decisions it names none for. */
const titles: Record<ActionName, string> = {
  accept: words.chooser.accept,
  dismiss: words.chooser.dismiss,
  complete: words.chooser.complete,
  wontDo: words.chooser.wontDo,
  reopen: words.chooser.reopen,
}

/** An action whose move has several values, such as Backlog or In
 * progress for an accept: each value by its declared label, and Cancel. */
export function Chooser() {
  const choosing = useStore(choosingStore)
  const model = useTasks()
  const actions = useTaskActions()
  const t = choosing ? model.task(choosing.path) : undefined
  if (!choosing || !t) return null
  const close = () => choosingStore.set(undefined)
  return (
    <Alert
      open
      title={titles[choosing.action]}
      message={t.item.title}
      onCancel={close}
      buttons={[
        ...choosing.choices.map((value) => ({
          label: valueLabel(t.item.collection, value),
          onClick: () => {
            close()
            actions.act(choosing.path, choosing.action, {
              to: value,
              edits: choosing.edits,
            })
          },
        })),
        {
          label: words.chooser.cancel,
          role: "cancel" as const,
          onClick: close,
        },
      ]}
    />
  )
}
