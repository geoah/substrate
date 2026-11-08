import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { Confirm, Sheet } from "./ui"

afterEach(cleanup)

function Opener(props: { onConfirm?: () => void }) {
  const [sheet, setSheet] = useState(false)
  const [asking, setAsking] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setSheet(true)}>
        Edit
      </button>
      <Sheet open={sheet} onClose={() => setSheet(false)} title="Edit Item">
        <input aria-label="Name" />
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
  it("name themselves and take focus when they open", () => {
    render(<Opener />)
    fireEvent.click(screen.getByText("Edit"))
    const dialog = screen.getByRole("dialog", { name: "Edit Item" })
    expect(dialog.getAttribute("aria-modal")).toBe("true")
    expect(document.activeElement).toBe(screen.getByLabelText("Cancel"))
  })

  it("keep Tab inside", () => {
    render(<Opener />)
    fireEvent.click(screen.getByText("Edit"))
    const last = screen.getByText("Delete")
    last.focus()
    fireEvent.keyDown(document.activeElement!, { key: "Tab" })
    expect(document.activeElement).toBe(screen.getByLabelText("Cancel"))
    fireEvent.keyDown(document.activeElement!, { key: "Tab", shiftKey: true })
    expect(document.activeElement).toBe(last)
  })

  it("start a question on Cancel, close only the top one on Escape, and give focus back", () => {
    const onConfirm = vi.fn()
    render(<Opener onConfirm={onConfirm} />)
    const opener = screen.getByText("Edit")
    opener.focus()
    fireEvent.click(opener)
    const deleteButton = screen.getByText("Delete")
    deleteButton.focus()
    fireEvent.click(deleteButton)
    const question = screen.getByRole("alertdialog", {
      name: "Delete this item?",
    })
    expect(question.getAttribute("aria-describedby")).toBeTruthy()
    expect(document.activeElement?.textContent).toBe("Cancel")

    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(screen.getByRole("dialog")).toBeTruthy()
    expect(document.activeElement).toBe(deleteButton)

    fireEvent.keyDown(document.activeElement!, { key: "Escape" })
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(document.activeElement).toBe(opener)
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
