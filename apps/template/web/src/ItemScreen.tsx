import {
  ActionRow,
  BarButton,
  Confirm,
  Field,
  LargeTitle,
  NavBar,
  ProblemBox,
  Row,
  Section,
  Sheet,
  Spinner,
  useKindWrites,
  useRecord,
} from "@substrate/react"
import { useState } from "react"
import { Item, itemTitle } from "./kinds"
import { WriteState } from "./WriteState"

/** One item: its name, Edit and Delete. An edit holds to the version the
 * owner opened it at, so a change made elsewhere meanwhile comes back as a
 * conflict to resolve in Settings, never written over. */
export function ItemScreen(props: { id: string; onBack: () => void }) {
  const read = useRecord(Item, props.id)
  const writes = useKindWrites(Item)
  const [editing, setEditing] = useState<Item | null>(null)
  const [name, setName] = useState("")
  const [notKept, setNotKept] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)

  const item = read.value
  if (!item) {
    return (
      <div className="mx-auto w-full max-w-2xl">
        <NavBar onBack={props.onBack} backLabel="Items" />
        {read.state === "loading" ? (
          <div className="flex justify-center py-16 text-secondary">
            <Spinner />
          </div>
        ) : read.state === "failed" ? (
          <div className="px-4 py-8">
            <ProblemBox
              title="Couldn't read this item"
              message={read.words}
              action={{ title: "Try Again", onClick: read.retry }}
            />
          </div>
        ) : (
          <p className="px-4 py-16 text-center text-[17px] text-secondary">
            This item is not in your repository.
          </p>
        )}
      </div>
    )
  }

  const open = () => {
    setEditing(item)
    setName(item.name)
    setNotKept(null)
  }

  const save = async () => {
    if (!editing) return
    const written = await writes.edit(editing.id, { name }, editing.version, {
      onNotKept: setNotKept,
    })
    if (written.kept) setEditing(null)
  }

  const unsaved = !!editing && name !== editing.name
  const changed = unsaved && name.trim() !== editing.name && !!name.trim()

  return (
    <div className="mx-auto w-full max-w-2xl pb-16">
      <NavBar
        onBack={props.onBack}
        backLabel="Items"
        actions={<BarButton onClick={open}>Edit</BarButton>}
      />
      <LargeTitle className="break-words">{itemTitle(item)}</LargeTitle>
      <div className="px-4">
        <div className="mb-4 px-4">
          <WriteState id={item.id} />
        </div>
        <Section>
          <Row label="Name" value={item.name || "None"} />
        </Section>
        <Section>
          <ActionRow destructive onClick={() => setDeleting(true)}>
            Delete Item
          </ActionRow>
        </Section>
      </div>

      <Sheet
        open={!!editing}
        onClose={() => setEditing(null)}
        unsaved={unsaved}
        title="Edit Item"
        actions={
          <BarButton prominent disabled={!changed} onClick={() => void save()}>
            {notKept ? "Retry" : "Save"}
          </BarButton>
        }
      >
        <Section>
          <Field label="Name" value={name} onChange={setName} autoFocus />
        </Section>
        {notKept && (
          <div className="overflow-hidden rounded-[10px] bg-card">
            <ProblemBox
              role="alert"
              title="A change was not kept"
              message={notKept}
            />
          </div>
        )}
      </Sheet>

      <Confirm
        open={deleting}
        title="Delete this item?"
        message="It is deleted from your substrate."
        confirm="Delete"
        onCancel={() => setDeleting(false)}
        onConfirm={() => {
          setDeleting(false)
          void writes.remove(item.id).then((w) => w.kept && props.onBack())
        }}
      />
    </div>
  )
}
