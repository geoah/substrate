/** One A2UI surface drawn from its store: the `root` component, then each
 * child by id. A component the catalog does not know, a child id the
 * surface does not hold, or a tree that nests past reason draws nothing and
 * is said once through `onProblem`, never thrown: the screen is data an
 * agent wrote, and the rest of it still works. */

import { useMemo, useState, useSyncExternalStore, type ReactNode } from "react"

import { resolveAction } from "@/lib/a2ui/actions"
import { isRecord } from "@/lib/a2ui/coerce"
import type { A2UIComponent, SurfaceStore } from "@/lib/a2ui/store"
import type { ChildRef } from "@/lib/a2ui/tree"
import { openUrl } from "@/lib/a2ui/urls"
import { resolveBool, resolveText, type EvalContext } from "@/lib/a2ui/values"
import type { AppletAction } from "@/lib/api/types"

import * as Catalog from "./catalog"
import { Problem } from "./problem"
import {
  SurfaceContext,
  useSurface,
  type NodeProps,
  type RootProps,
  type SurfaceEnv,
} from "./context"

const COMPONENTS: Record<string, (props: NodeProps) => ReactNode> = {
  Text: Catalog.Text,
  Image: Catalog.Img,
  Icon: Catalog.Icon,
  Video: Catalog.Video,
  AudioPlayer: Catalog.AudioPlayer,
  Row: Catalog.Row,
  Column: Catalog.Column,
  List: Catalog.List,
  Card: Catalog.Card,
  Tabs: Catalog.TabsComponent,
  Modal: Catalog.Modal,
  Divider: Catalog.Divider,
  Button: Catalog.ButtonComponent,
  TextField: Catalog.TextField,
  CheckBox: Catalog.CheckBox,
  ChoicePicker: Catalog.ChoicePicker,
  Slider: Catalog.Slider,
  DateTimeInput: Catalog.DateTimeInput,
}

/** Deeper than any screen a person reads; a component that contains itself
 * stops here instead of exhausting the stack. */
const MAX_DEPTH = 48

const LIVE = new Set(["off", "polite", "assertive"])

function rootProps(def: A2UIComponent, ctx: EvalContext): RootProps {
  const out: RootProps = { "data-a2ui": def.component }
  if (typeof def.weight === "number" && def.weight > 0)
    out.style = { flex: `${def.weight} 1 0%`, minWidth: 0 }
  const a11y = def.accessibility
  if (isRecord(a11y)) {
    const label = resolveText(a11y.label, ctx)
    const description = resolveText(a11y.description, ctx)
    if (label) out["aria-label"] = label
    if (description) out["aria-description"] = description
    if (resolveBool(a11y.hidden, ctx)) out["aria-hidden"] = true
    if (typeof a11y.live === "string" && LIVE.has(a11y.live))
      out["aria-live"] = a11y.live as RootProps["aria-live"]
  }
  return out
}

function Node({
  id,
  ctx,
  depth,
}: {
  id: string
  ctx: EvalContext
  depth: number
}) {
  const env = useSurface()
  if (depth > MAX_DEPTH)
    return <Problem message={`${id}: nested too deep to draw`} />
  const def = env.components.get(id)
  if (!def) return <Problem message={`no component with the id “${id}”`} />
  const Component = Object.hasOwn(COMPONENTS, def.component)
    ? COMPONENTS[def.component]
    : undefined
  if (!Component)
    return (
      <Problem
        message={`${id}: “${def.component}” is not a component of the basic catalog`}
      />
    )
  return (
    <Component def={def} ctx={ctx} depth={depth} root={rootProps(def, ctx)} />
  )
}

export function A2UISurface({
  store,
  surfaceId,
  onAction,
  onProblem,
  busy = false,
  destructive,
  locale,
}: {
  store: SurfaceStore
  surfaceId: string
  /** A button's event, its context resolved at the tap. Stable, so the
   * surface's context does not change on every render of the host. */
  onAction: (action: AppletAction) => void
  /** Each thing of the screen that cannot be drawn, once it commits; the
   * host keeps the distinct ones. Stable, or every render says it again. */
  onProblem?: (message: string) => void
  busy?: boolean
  /** The applet's events that delete a record. */
  destructive?: readonly string[]
  locale?: string
}) {
  const surface = useSyncExternalStore(store.subscribe, () =>
    store.surface(surfaceId)
  )
  // The button whose event was sent last; it reads as running while the
  // host is busy.
  const [tapped, setTapped] = useState<{ id: string; scope?: string }>()
  const destructiveKey = JSON.stringify(destructive ?? [])
  const deleting = useMemo(
    () => new Set(JSON.parse(destructiveKey) as string[]),
    [destructiveKey]
  )

  const env = useMemo<SurfaceEnv | undefined>(() => {
    if (!surface) return undefined
    return {
      components: surface.components,
      busy,
      pending: busy ? tapped : undefined,
      destructive: deleting,
      locale,
      write: (path, value) => store.write(surfaceId, path, value),
      act: (action, ctx, sourceComponentId) => {
        // The context is read at the tap, from the model as it is now.
        const model = store.surface(surfaceId)?.dataModel ?? ctx.model
        const resolved = resolveAction(
          action,
          { ...ctx, model },
          sourceComponentId
        )
        if (resolved.type === "event") {
          setTapped({ id: sourceComponentId, scope: ctx.scope })
          onAction(resolved.action)
        } else if (resolved.type === "openUrl") {
          if (!openUrl(resolved.url))
            onProblem?.(
              `${sourceComponentId}: only a web address can be opened`
            )
        } else if (resolved.problem) onProblem?.(resolved.problem)
      },
      problem: (message) => onProblem?.(message),
      child: (ref: ChildRef, depth: number) => (
        <Node key={ref.key} id={ref.id} ctx={ref.ctx} depth={depth} />
      ),
    }
  }, [
    surface,
    busy,
    tapped,
    deleting,
    locale,
    store,
    surfaceId,
    onAction,
    onProblem,
  ])

  if (!surface || !env) return null
  return (
    <SurfaceContext value={env}>
      <div data-a2ui-surface={surfaceId} className="flex min-w-0 flex-col">
        <Node id="root" ctx={{ model: surface.dataModel, locale }} depth={0} />
      </div>
    </SurfaceContext>
  )
}
