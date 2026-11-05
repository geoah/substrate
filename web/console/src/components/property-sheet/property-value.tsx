/** One property's value, read: the shape its datatype earns. A declared
 * object, a list of them and a keyed map read as their structure
 * (structured-value.tsx), as deep as the declaration goes; every other value
 * reads as itself (leaf-value.tsx); and a shape nobody declared fields for
 * is pretty-printed JSON. History, review and the record views read values
 * through this, so a value reads the same everywhere it is shown. */

import { Empty, LeafValue, LooseValue } from "./leaf-value"
import { StructuredValue } from "./structured-value"
import { isStructured } from "@/lib/structured-layout"
import type { PropSpec } from "@/lib/record-schema"

export { Empty, LooseValue }

/** A declared property's value, container and all. */
export function DeclaredValue({
  spec,
  value,
}: {
  spec: PropSpec
  value: unknown
}) {
  if (value === undefined || value === null || value === "") return <Empty />
  if (spec.kind !== "secret" && (isStructured(spec) || spec.keyed)) {
    return <StructuredValue spec={spec} value={value} />
  }
  return <LeafValue spec={spec} value={value} />
}
