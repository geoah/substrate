/** A record link's mark inside the editor: the record's chip, which reads the
 * live title. The stored title is only what other Markdown readers show. */

import { NodeViewWrapper, type ReactNodeViewProps } from "@tiptap/react"

import { RecordRef } from "@/components/identity/record-ref"
import type { RecordLinkAttrs } from "./record-link"

export function RecordLinkView({ node }: ReactNodeViewProps) {
  const { kind, id } = node.attrs as RecordLinkAttrs
  return (
    <NodeViewWrapper as="span" data-record-chip="">
      <RecordRef kind={kind} id={id} variant="chip" />
    </NodeViewWrapper>
  )
}
