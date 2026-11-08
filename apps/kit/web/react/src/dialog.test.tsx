import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { Confirm, Field, Sheet } from "./ui"

afterEach(cleanup)

function Opener(props: { onConfirm?: () => void }) {
  const [sheet, setSheet] = useState(false)
  const [asking, setAsking] = useState(false)
  const [name, setName] = useState("")
  return (
    <>
      <button type="button" onClick={() => setSheet(true)}>
        Edit
      </button>
      <Sheet open={sheet} onClose={() => setSheet(false)} title="Edit Item">
        <Field label="Name" value={name} onChange={setName} autoFocus />
        <button type="button" onClick={() => setAsking(true)}>
          Delete
        </button>
      </Sheet>
      <Confirm
        open={asking}
        title="Delete this item?"
        message="It goes from your substrate."
        confirm="Delete"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false)
          props.onConfirm?.()
        }}
      />
    </>
  )
}

describe("the dialogs", () => {
  it("open in a portal, named, with the page behind them inert, and focus on the field marked", async () => {
    const { container } = render(<Opener />)
    fireEvent.click(screen.getByText("Edit"))
    const dialog = await screen.findByRole("dialog", { name: "Edit Item" })
    expect(container.contains(dialog)).toBe(false)
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByLabelText("Name"))
    )
    await waitFor(() =>
      expect(
        container.closest("[inert], [aria-hidden=true]") ??
          container.parentElement?.closest("[inert], [aria-hidden=true]") ??
          (container.hasAttribute("inert") ||
          container.getAttribute("aria-hidden") === "true"
            ? container
            : null)
      ).not.toBeNull()
    )
  })

  it("start a question on Cancel, close only the top one on Escape, and give focus back", async () => {
    const onConfirm = vi.fn()
    render(<Opener onConfirm={onConfirm} />)
    const opener = screen.getByText("Edit")
    opener.focus()
    fireEvent.click(opener)
    const deleteButton = await screen.findByText("Delete")
    deleteButton.focus()
    fireEvent.click(deleteButton)
    const question = await screen.findByRole("alertdialog", {
      name: "Delete this item?",
    })
    expect(question.getAttribute("aria-describedby")).toBeTruthy()
    await waitFor(() =>
      expect(document.activeElement?.textContent).toBe("Cancel")
    )

    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(screen.getByRole("dialog")).toBeTruthy()
    await waitFor(() => expect(document.activeElement).toBe(deleteButton))

    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(opener))
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
