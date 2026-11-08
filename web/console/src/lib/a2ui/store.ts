/** The renderer's half of A2UI v1.0 (https://a2ui.org): the surfaces the
 * agent's messages build, each a flat map of components by id and one data
 * model. A surface is replaced, never mutated, so a React snapshot read
 * through `useSyncExternalStore` stays the tree it drew. */

import type { A2UIMessage } from "@/lib/api/types"

import { removeAt, setAt } from "./pointer"

/** One component as the agent declared it: an id, a type from the catalog
 * and the type's props, read by the component that draws it. */
export interface A2UIComponent extends Record<string, unknown> {
  id: string
  component: string
}

export interface Surface {
  id: string
  catalogId: string
  components: ReadonlyMap<string, A2UIComponent>
  dataModel: Record<string, unknown>
}

type WriteListener = (surfaceId: string, path: string) => void

function asComponents(list: unknown, problems: string[]): A2UIComponent[] {
  if (!Array.isArray(list)) return []
  const out: A2UIComponent[] = []
  for (const item of list) {
    if (
      item &&
      typeof item === "object" &&
      typeof (item as A2UIComponent).id === "string" &&
      typeof (item as A2UIComponent).component === "string"
    ) {
      out.push(item as A2UIComponent)
    } else {
      problems.push("a component without an id or a type was skipped")
    }
  }
  return out
}

function asModel(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

export class SurfaceStore {
  private surfaces = new Map<string, Surface>()
  private listeners = new Set<() => void>()
  private writeListeners = new Set<WriteListener>()

  /** Applies messages in order and answers what could not be applied. A
   * message for a surface that was never created is dropped, as the
   * protocol requires `createSurface` first. */
  process(messages: readonly A2UIMessage[]): string[] {
    const problems: string[] = []
    let changed = false
    for (const message of messages) {
      if (message.createSurface) {
        const m = message.createSurface
        const components = new Map<string, A2UIComponent>()
        for (const c of asComponents(m.components, problems))
          components.set(c.id, c)
        this.surfaces.set(m.surfaceId, {
          id: m.surfaceId,
          catalogId: m.catalogId,
          components,
          dataModel: asModel(m.dataModel),
        })
        changed = true
      } else if (message.updateComponents) {
        const m = message.updateComponents
        const surface = this.surfaces.get(m.surfaceId)
        if (!surface) {
          problems.push(`components for unknown surface “${m.surfaceId}”`)
          continue
        }
        const components = new Map(surface.components)
        for (const c of asComponents(m.components, problems))
          components.set(c.id, c)
        this.surfaces.set(m.surfaceId, { ...surface, components })
        changed = true
      } else if (message.updateDataModel) {
        const m = message.updateDataModel
        if (!this.surfaces.has(m.surfaceId)) {
          problems.push(`data for unknown surface “${m.surfaceId}”`)
          continue
        }
        this.writeData(m.surfaceId, m.path ?? "/", m.value)
        changed = true
      } else if (message.deleteSurface) {
        changed = this.surfaces.delete(message.deleteSurface.surfaceId)
      } else {
        problems.push("a message with no body was skipped")
      }
    }
    if (changed) this.notify()
    return problems
  }

  surface(id: string): Surface | undefined {
    return this.surfaces.get(id)
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  /** Hears every write an input makes, with its path, after it lands. */
  onInputWrite(fn: WriteListener): () => void {
    this.writeListeners.add(fn)
    return () => this.writeListeners.delete(fn)
  }

  /** An input's two-way write: the local data model only, no network. */
  write(surfaceId: string, path: string, value: unknown): void {
    if (!this.surfaces.has(surfaceId)) return
    this.writeData(surfaceId, path, value)
    this.notify()
    for (const fn of this.writeListeners) fn(surfaceId, path)
  }

  /** A null or undefined value removes the key, as `updateDataModel` reads
   * it. */
  private writeData(surfaceId: string, path: string, value: unknown) {
    const surface = this.surfaces.get(surfaceId)!
    const next =
      value === null || value === undefined
        ? removeAt(surface.dataModel, path)
        : setAt(surface.dataModel, path, value)
    this.surfaces.set(surfaceId, { ...surface, dataModel: asModel(next) })
  }

  private notify() {
    for (const fn of this.listeners) fn()
  }
}
