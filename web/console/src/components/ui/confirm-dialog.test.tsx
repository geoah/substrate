// @vitest-environment jsdom
/** One confirmation for every consequence: the question, the consequence,
 * the verb; a running action holds the dialog open. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ConfirmDialog, PauseDialog, SignOutDialog } from "./confirm-dialog"

afterEach(cleanup)

describe("ConfirmDialog", () => {
  it("asks, names the consequence and confirms with its verb", async () => {
    const onConfirm = vi.fn()
    const onClose = vi.fn()
    render(
      <ConfirmDialog
        title="Delete “Groceries”?"
        consequence="It’s removed from Notes."
        confirm="Delete"
        destructive
        onConfirm={onConfirm}
        onClose={onClose}
      />
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Delete “Groceries”?")
    expect(dialog.textContent).toContain("It’s removed from Notes.")
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    expect(onConfirm).toHaveBeenCalledOnce()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onClose).toHaveBeenCalledOnce()
  })

  it("waits while the action runs and cannot be dismissed", async () => {
    const onConfirm = vi.fn()
    const onClose = vi.fn()
    render(
      <ConfirmDialog
        title="Remove it?"
        consequence="Gone."
        confirm="Remove"
        pending
        onConfirm={onConfirm}
        onClose={onClose}
      />
    )
    const dialog = await screen.findByRole("dialog")
    const buttons = screen.getAllByRole("button", { name: /Cancel|Remove/ })
    for (const b of buttons)
      expect((b as HTMLButtonElement).disabled).toBe(true)
    fireEvent.keyDown(dialog, { key: "Escape" })
    expect(onClose).not.toHaveBeenCalled()
  })

  it("says what went wrong as an alert", async () => {
    render(
      <ConfirmDialog
        title="Delete it?"
        consequence="Gone."
        confirm="Delete"
        error="The server refused."
        onConfirm={() => {}}
        onClose={() => {}}
      />
    )
    expect((await screen.findByRole("alert")).textContent).toBe(
      "The server refused."
    )
  })
})

describe("the shared confirmations", () => {
  it("pauses anything with one sentence", async () => {
    const onConfirm = vi.fn()
    render(
      <PauseDialog
        name="Google Contacts sync"
        onConfirm={onConfirm}
        onClose={() => {}}
      />
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Pause Google Contacts sync?")
    expect(dialog.textContent).toContain(
      "It won’t run on its own until you resume it. Nothing it brought in changes."
    )
    fireEvent.click(screen.getByRole("button", { name: "Pause" }))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it("signs this browser out with the same words everywhere", async () => {
    render(<SignOutDialog onConfirm={() => {}} onClose={() => {}} />)
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Sign out?")
    expect(dialog.textContent).toContain(
      "This browser will need your password again."
    )
  })
})
