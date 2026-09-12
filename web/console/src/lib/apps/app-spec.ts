/** One app record → one `AppSpec`, with what only the COMPOSITION knows: a
 * `$input` a screen's view uses that the app does not declare, a screen whose
 * view is missing, and a second `home: true` (the lowest id wins). A view's
 * own problems stay on the view. */

import { KIND_RECORD_PREFIX, VIEW_RECORD_PREFIX } from "@/lib/api/apps"
import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { humanizeName } from "@/lib/record-schema"
import type { AppSpec, InputSpec, Problem, ScreenSpec } from "./spec"
import { tokenInputs } from "./tokens"
import { refTarget, viewSpec } from "./view-spec"

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() ? v : undefined
}

function obj(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {}
}

export function appSpec(
  record: SubstrateRecord,
  views: SubstrateRecord[],
  kinds: KindInfo[],
  apps: SubstrateRecord[] = []
): AppSpec {
  const p = record.properties
  const problems: Problem[] = []

  const inputs: Record<string, InputSpec> = {}
  for (const [name, raw] of Object.entries(obj(p.inputs))) {
    const input = obj(raw)
    inputs[name] = {
      kind: refTarget(input.kind, KIND_RECORD_PREFIX),
      description: str(input.description),
    }
    if (!inputs[name].kind) {
      problems.push({
        path: `inputs.${name}.kind`,
        message: "an input names the kind whose records satisfy it",
        severity: "error",
      })
    }
  }

  const bindings: Record<string, string> = {}
  for (const [name, raw] of Object.entries(obj(p.bindings))) {
    const held = readReference(raw)
    if (held) bindings[name] = held.path
  }

  const screens: ScreenSpec[] = []
  const rawScreens = Array.isArray(p.screens) ? p.screens : []
  rawScreens.forEach((raw, i) => {
    const screen = obj(raw)
    const name = str(screen.name)
    const viewId = refTarget(screen.view, VIEW_RECORD_PREFIX)
    if (!name || !viewId) {
      problems.push({
        path: `screens[${i}]`,
        message: "a screen needs a name and a view",
        severity: "error",
      })
      return
    }
    const view = views.find((v) => v.id === viewId)
    if (!view) {
      problems.push({
        path: `screens[${i}].view`,
        message: `no view ${JSON.stringify(viewId)}`,
        severity: "error",
      })
    } else {
      const spec = viewSpec(view, kinds)
      for (const input of tokenInputs(spec)) {
        if (!(input in inputs)) {
          problems.push({
            path: `screens[${i}].view`,
            message: `${viewId} uses $input.${input}, which this app does not declare`,
            severity: "error",
          })
        }
      }
    }
    screens.push({
      name,
      label:
        str(screen.label) ?? str(view?.properties.name) ?? humanizeName(name),
      icon: str(screen.icon),
      view: viewId,
    })
  })
  if (!screens.length) {
    problems.push({
      path: "screens",
      message: "an app has at least one screen",
      severity: "error",
    })
  }

  const home = p.home === true
  if (home) {
    const other = apps
      .filter((a) => a.id !== record.id && a.properties.home === true)
      .map((a) => a.id)
      .sort()[0]
    if (other !== undefined && other < record.id) {
      problems.push({
        path: "home",
        message: `${other} is home too, and the lowest id wins`,
        severity: "warning",
      })
    }
  }

  return {
    id: record.id,
    name: str(p.name) ?? record.id,
    description: str(p.description),
    icon: str(p.icon),
    home,
    screens,
    inputs,
    bindings,
    problems,
  }
}
