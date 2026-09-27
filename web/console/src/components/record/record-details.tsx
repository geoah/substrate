/** The technical "Record" section: the full reference, the kind and the
 * version it was written under, the id and any former ids, and who created
 * and last changed it, raw actor ids included. */

import type { ReactNode } from "react"

import { ago } from "@/components/property-sheet/dates"
import { ActorRef } from "@/components/identity/actor-ref"
import { IdText } from "@/components/identity/id-text"
import { KindRef } from "@/components/identity/kind-ref"
import type { ChangeRow, KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindPurpose } from "@/lib/definition"

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="contents">
      <dt className="text-faint">{label}</dt>
      <dd className="m-0 flex min-w-0 flex-wrap items-center gap-1.5">
        {children}
      </dd>
    </div>
  )
}

export function RecordDetails({
  record,
  kind,
  rows,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  rows: ChangeRow[]
}) {
  const created = rows.find((r) => r.payload?.created === true)
  const latest = rows[0]
  const version = record.kindVersion ?? kind?.version
  return (
    <dl
      data-slot="record-details"
      className="grid grid-cols-[130px_minmax(0,1fr)] gap-x-3.5 gap-y-2 text-[13px]"
    >
      <Fact label="Reference">
        <IdText value={`${record.kind}/${record.id}`} copy />
      </Fact>
      <Fact label="Kind">
        <KindRef kind={kind ?? record.kind} mode="reference" />
        <span className="text-faint">
          {version ? `version ${version} · ` : ""}
          {kindPurpose(kind ?? record.kind)}
        </span>
      </Fact>
      <Fact label="ID">
        <IdText value={record.id} copy />
        {(record.formerIds ?? []).length > 0 && (
          <>
            <span className="text-faint">formerly</span>
            {record.formerIds!.map((id) => (
              <IdText key={id} value={id} />
            ))}
          </>
        )}
      </Fact>
      <Fact label="Created">
        {created && <ActorRef actor={created.actor} />}
        <span className="text-faint" title={record.createdAt}>
          {ago(record.createdAt)}
        </span>
      </Fact>
      <Fact label="Last changed">
        {latest && <ActorRef actor={latest.actor} />}
        <span className="text-faint" title={record.updatedAt}>
          {ago(record.updatedAt)} · version {record.version}
        </span>
      </Fact>
    </dl>
  )
}
