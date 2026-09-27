/** The record's source, in technical mode: the YAML envelope (references
 * linked, the way to edit it beside it) or the JSON the API serves, for a
 * developer who reads or pipes the wire. */

import { useMemo, useState } from "react"
import { Link } from "@tanstack/react-router"
import { PencilIcon } from "lucide-react"

import { YamlView } from "./yaml-view"
import { CodeBlock } from "@/components/code-block"
import { CopyButton } from "@/components/identity/copy-button"
import { Button } from "@/components/ui/button"
import { Segmented } from "@/components/ui/segmented"
import { splitKind } from "@/lib/api/http"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { linkTargetsOf, manifestYAML } from "@/lib/manifest"
import { keyDocsOf } from "@/lib/yaml-annotations"

export function SourceView({
  record,
  kind,
  kinds,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: KindInfo[]
}) {
  const [lens, setLens] = useState<"yaml" | "json">("yaml")
  const docs = useMemo(() => keyDocsOf(kind), [kind])
  const yaml = useMemo(() => manifestYAML(record), [record])
  const json = useMemo(() => JSON.stringify(record, null, 2), [record])
  const targets = useMemo(() => linkTargetsOf(record, kinds), [record, kinds])
  const { authority, pkg, name } = splitKind(record.kind)
  return (
    <div data-slot="record-source" className="mt-[18px] flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Segmented
          label="Show the record as"
          value={lens}
          options={[
            { value: "yaml", label: "YAML" },
            { value: "json", label: "JSON" },
          ]}
          onChange={setLens}
        />
        <span className="text-[12.5px] text-faint">
          {lens === "yaml"
            ? "The record as its YAML envelope. A linked reference opens its record."
            : "The record as the API serves it."}
        </span>
        <span className="ml-auto flex items-center gap-1">
          {lens === "json" && (
            <CopyButton value={json} label="Copy the record’s JSON" />
          )}
          <Button
            variant="outline"
            size="sm"
            render={
              <Link
                to="/data/$authority/$pkg/$name/$id/edit"
                params={{ authority, pkg, name, id: record.id }}
              />
            }
          >
            <PencilIcon />
            Edit YAML
          </Button>
        </span>
      </div>
      <div className="overflow-hidden rounded-lg border bg-panel">
        {lens === "yaml" ? (
          <YamlView source={yaml} docs={docs} targets={targets} />
        ) : (
          <CodeBlock
            lang="json"
            source={json}
            className="rounded-none bg-transparent px-3 py-2.5"
          />
        )}
      </div>
    </div>
  )
}
