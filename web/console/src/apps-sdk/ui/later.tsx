/** The v1 components, exported today so an import never fails to resolve:
 * each renders a note in place of itself. The prop shapes are the contract
 * already, so an app written against them keeps compiling when they land. */

import type { ComponentProps, FC, ReactNode } from "react"

import type { SubstrateRecord } from "@/lib/api/types"
import type { Page } from "./sdk"

function Later({ name }: { name: string }) {
  return (
    <div className="kit-later" role="note">
      {name} arrives in v1.
    </div>
  )
}

export interface ButtonProps extends ComponentProps<"button"> {
  variant?: "default" | "outline" | "ghost" | "destructive"
  size?: "sm" | "md" | "lg"
}

export const Button: FC<ButtonProps> = () => <Later name="Button" />

export interface SheetProps {
  open: boolean
  onOpenChange(open: boolean): void
  title?: ReactNode
  footer?: ReactNode
  children?: ReactNode
}

export const Sheet: FC<SheetProps> = () => <Later name="Sheet" />

export interface FormProps {
  kind: string
  record?: SubstrateRecord
  prompt?: string[]
  defaults?: Record<string, unknown>
  onSubmitted?(record: SubstrateRecord): void
  onCancel?(): void
}

export const Form: FC<FormProps> = () => <Later name="Form" />

export interface RecordFieldProps {
  record: SubstrateRecord
  property: string
}

export const RecordField: FC<RecordFieldProps> = () => (
  <Later name="RecordField" />
)

export interface BoardProps<T extends SubstrateRecord = SubstrateRecord> {
  page: Page
  property: string
  onMove?(record: T, to: string): void
}

export const Board: FC<BoardProps> = () => <Later name="Board" />
