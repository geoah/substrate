/** The form layout: a standalone create screen for the view's kind, over the
 * first `create` action's `prompt`. Seeded from `set`, from `via` (the
 * parent, when the screen mounts it under one) and from every `eq` on a
 * writable property in the filter, so the record lands where the view would
 * show it; the write carries an idempotency key and a successful submit
 * toasts and clears for the next. A view with no create action asks for
 * every owner-writable property, which is what a bare form over a kind is. */

import { PackageOpenIcon } from "lucide-react"

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { KindInfo } from "@/lib/api/types"
import type { ActionHost, ActionSpec, LayoutProps } from "@/lib/apps/spec"
import { ownerWritable, propSpecs } from "@/lib/record-schema"
import { CreateForm } from "./shared"

/** The create a form falls back to: everything the owner may write. */
function wholeKind(kind: KindInfo): ActionSpec {
  return {
    name: "create",
    label: `Add ${kind.name}`,
    verb: "create",
    placement: "primary",
    prompt: propSpecs(kind)
      .filter((s) => ownerWritable(s) && !s.managed)
      .map((s) => s.name),
    set: {},
    confirm: false,
  }
}

export default function FormLayout({ spec, kind, kinds, ctx }: LayoutProps) {
  if (!kind) {
    return (
      <Empty className="py-10">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <PackageOpenIcon />
          </EmptyMedia>
          <EmptyTitle>{spec.kind ?? "This kind"} is not installed</EmptyTitle>
          <EmptyDescription>
            Import or install its package and this form renders.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  const action =
    spec.actions.find((a) => a.verb === "create") ?? wholeKind(kind)
  const host: ActionHost = { spec, kind, kinds, ctx }
  return (
    <div className="mx-auto flex w-full max-w-lg flex-col px-4 py-4 md:px-6">
      <CreateForm host={host} action={action} idPrefix={`form-${spec.id}`} />
    </div>
  )
}
