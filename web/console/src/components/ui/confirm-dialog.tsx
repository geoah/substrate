/** The one confirmation: a question for a title, the consequence in a
 * sentence, and a button that says the verb. While the action runs the
 * dialog cannot be dismissed, so a result never lands on a closed dialog. */

import type { ReactNode } from "react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { cn } from "@/lib/utils"

export interface ConfirmDialogProps {
  /** Controlled open state; a dialog rendered conditionally leaves it on. */
  open?: boolean
  /** A question: "Delete “Groceries”?" */
  title: ReactNode
  /** What happens, and what does not. */
  consequence: ReactNode
  /** The confirm button's verb: "Delete", "Sign out". */
  confirm: ReactNode
  /** Red confirm, for what cannot be undone. */
  destructive?: boolean
  /** The action is running: both buttons wait and the dialog stays. */
  pending?: boolean
  /** Holds the confirm back for a reason of the caller's own. */
  disabled?: boolean
  /** What went wrong, said under the consequence. */
  error?: ReactNode
  /** Anything the reader should see or fill in before confirming. */
  children?: ReactNode
  onConfirm: () => void
  /** Cancel, Escape, the backdrop, the close button: never while pending. */
  onClose: () => void
  className?: string
}

export function ConfirmDialog({
  open = true,
  title,
  consequence,
  confirm,
  destructive = false,
  pending = false,
  disabled = false,
  error,
  children,
  onConfirm,
  onClose,
  className,
}: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && !pending && onClose()}>
      <DialogContent
        data-slot="confirm-dialog"
        className={cn("sm:max-w-md", className)}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{consequence}</DialogDescription>
        </DialogHeader>
        {children}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" disabled={pending} onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant={destructive ? "destructive" : "default"}
            disabled={pending || disabled}
            onClick={onConfirm}
          >
            {pending && <Spinner className="size-3.5" />}
            {confirm}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Pausing anything that runs on its own (a tool, a provider, an account's
 * sync) is one decision, so it is one dialog. */
export function PauseDialog({
  name,
  pending,
  onConfirm,
  onClose,
}: {
  /** What pauses, as the reader knows it: "Google Contacts sync". */
  name: string
  pending?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  return (
    <ConfirmDialog
      title={`Pause ${name}?`}
      consequence="It won’t run on its own until you resume it. Nothing it brought in changes."
      confirm="Pause"
      pending={pending}
      onConfirm={onConfirm}
      onClose={onClose}
    />
  )
}

/** Signing this browser out, from the account menu or from Settings. */
export function SignOutDialog({
  pending,
  onConfirm,
  onClose,
}: {
  pending?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  return (
    <ConfirmDialog
      title="Sign out?"
      consequence="This browser will need your password again."
      confirm="Sign out"
      destructive
      pending={pending}
      onConfirm={onConfirm}
      onClose={onClose}
    />
  )
}
