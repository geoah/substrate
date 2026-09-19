/** "Linked from": the records that map ONTO the one being read.
 *
 * A recordmapping synthesises its subject slot on the SOURCE kind (decision
 * 0085), so a person is pointed at by a Slack user, a GitHub user and a Google
 * contact and NOTHING on the person says so — the owner's review of Mneme v6
 * called that out: "there's no link in the manifest to point to the other
 * mapped things". The single-record read carries them as `linkedFrom`
 * (decision 0088) and this is where the manifest shows them, under the
 * properties, because they read as part of what the record IS rather than as
 * the graph walk the Graph tab offers.
 *
 * One row per source record: its RecordPill — the one way a record is
 * referenced anywhere, so the title is the label and the whole pill is the
 * link — with the kind it is a record of and the mapping that owns the slot
 * beside it, both muted. The kind is repeated per row rather than heading a
 * group: the list is short by construction (one mirror per source record that
 * converged here) and a group header would cost more than it saves. */

import { RecordPill } from "@/components/record-pill"
import { splitKind } from "@/lib/api/http"
import { type KindInfo, type LinkedRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"

/** One row. The pill is inert when the registry does not know the kind — an
 * uninstalled kind renders rather than minting a dead link, exactly as the
 * graph's nodes do. */
function LinkRow({ link, kinds }: { link: LinkedRecord; kinds: KindInfo[] }) {
  const target = splitRecordPath(link.ref)
  const routable = Boolean(kindByIdentity(kinds, link.kind))
  return (
    <li className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
      <RecordPill
        kind={routable ? link.kind : ""}
        id={target?.id ?? link.ref}
        title={link.title || undefined}
        className="min-w-0"
      />
      <span className="data text-xs text-muted-foreground" title={link.kind}>
        {splitKind(link.kind).name || link.kind}
      </span>
      <span
        className="truncate data text-xs text-muted-foreground/70"
        title={`${link.mapping} fills ${link.property} on ${link.kind}`}
      >
        {splitKind(link.mapping).name || link.mapping}
      </span>
    </li>
  )
}

export function LinkedFromSection({
  links,
  kinds,
}: {
  /** The record's `linkedFrom`, already ordered by kind then id. */
  links: LinkedRecord[]
  /** The registry, so a source kind resolves to a route. */
  kinds: KindInfo[]
}) {
  if (!links.length) return null
  return (
    <section className="flex min-w-0 flex-col gap-2 border-t pt-4">
      <div className="flex items-baseline gap-2">
        <h3 className="text-sm font-medium">Linked from</h3>
        <span className="text-xs text-muted-foreground">
          the records a mapping links to this one
        </span>
      </div>
      <ul className="flex flex-col gap-1.5">
        {links.map((link) => (
          <LinkRow
            key={`${link.ref} ${link.property}`}
            link={link}
            kinds={kinds}
          />
        ))}
      </ul>
    </section>
  )
}
