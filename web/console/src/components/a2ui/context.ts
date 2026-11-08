/** What every drawn A2UI component reads from the surface it is part of. */

import {
  createContext,
  useContext,
  type CSSProperties,
  type ReactNode,
} from "react"

import type { A2UIComponent } from "@/lib/a2ui/store"
import type { ChildRef } from "@/lib/a2ui/tree"
import type { EvalContext } from "@/lib/a2ui/values"

export interface SurfaceEnv {
  components: ReadonlyMap<string, A2UIComponent>
  /** An action is running: a button that sends one waits for it. */
  busy: boolean
  /** The button whose tap is running, by its id and its template scope: a
   * list's rows share one id. */
  pending?: { id: string; scope?: string }
  /** The events that delete a record: a button that sends one is drawn as
   * destructive. */
  destructive: ReadonlySet<string>
  locale?: string
  /** An input's two-way write, at an absolute pointer. */
  write: (path: string, value: unknown) => void
  /** A button's `action`, run in the button's scope. */
  act: (action: unknown, ctx: EvalContext, sourceComponentId: string) => void
  /** Something of the screen that cannot be drawn, said once. */
  problem: (message: string) => void
  /** One child, drawn one level deeper. */
  child: (ref: ChildRef, depth: number) => ReactNode
}

export const SurfaceContext = createContext<SurfaceEnv | null>(null)

/** Drawn inside a button, where only phrasing content belongs: a Text, a
 * Row or a Column is a span there, never a paragraph, a heading or a
 * div. */
export const PhrasingContext = createContext(false)

export function useSurface(): SurfaceEnv {
  const env = useContext(SurfaceContext)
  if (!env) throw new Error("an A2UI component drawn outside its surface")
  return env
}

/** What the surface hands every component: its declaration, where it reads,
 * how deep it sits, and the props its root element carries (weight,
 * accessibility). */
export interface NodeProps {
  def: A2UIComponent
  ctx: EvalContext
  depth: number
  root: RootProps
}

export interface RootProps {
  style?: CSSProperties
  "aria-label"?: string
  "aria-description"?: string
  "aria-hidden"?: boolean
  "aria-live"?: "off" | "polite" | "assertive"
  "data-a2ui": string
}
