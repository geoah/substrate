import { describe } from "@substrate/client"
import { ProblemBox } from "@substrate/react"
import { useEffect, useMemo } from "react"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import { formatDate } from "../lib/format"
import {
  organizationName,
  providersOf,
  type ClinicalRecord,
  type ClinicalRow,
} from "../record/read"
import { go, useRoute } from "../route"
import { isUnread, rowKey } from "../rules/record"
import { sortFindings, type Finding } from "../rules/types"
import { Badge } from "../ui/Badge"
import { Disclosure } from "../ui/Disclosure"
import { TextButton } from "../ui/IconButton"
import { Screen } from "../ui/Screen"
import { SkeletonRow } from "../ui/Skeleton"
import {
  FindingRow,
  PickerPill,
  RecordCard,
  RecordRow,
  RecordSection,
  RowDate,
  useFirstTen,
} from "../ui/record/parts"
import {
  AllergyRow,
  AppointmentRow,
  ConsultationRow,
  DocumentRow,
  MessageRow,
  ReferralRow,
  sentences,
  str,
  word,
  type OpenFile,
} from "../ui/record/rows"

/** The clinical record from every provider the repository holds: what
 * needs attention, then conditions, medicines, allergies, vaccinations,
 * appointments, referrals, documents, consultations and messages. A
 * finding links here with `section` to scroll to and `open` naming the row
 * to unfold. */
export function RecordScreen() {
  const backend = useBackend()
  const record = backend.record.useClinicalRecord()
  const findings = backend.record.useRecordFindings()
  const seen = backend.record.useRecordSeen()
  const openFile = backend.record.useOpenFile()
  const route = useRoute()
  const query = new URLSearchParams(
    route.screen === "record" ? route.query : ""
  )
  const provider = query.get("provider") ?? "all"
  const section = query.get("section")
  const openKey = query.get("open") ?? undefined

  useEffect(() => {
    if (!section || !record.data) return
    const target =
      (openKey &&
        document.getElementById(`row-${openKey.replace(":", "-")}`)) ||
      document.getElementById(`record-${section}`)
    target?.scrollIntoView({ block: "start" })
  }, [section, openKey, record.data])

  const providers = useMemo(
    () => (record.data ? providersOf(record.data) : []),
    [record.data]
  )
  const setProvider = (p: string) => {
    const q = new URLSearchParams()
    if (p !== "all") q.set("provider", p)
    go({ screen: "record", query: q.toString() }, true)
  }

  return (
    <Screen
      title="Record"
      subtitle={
        providers.length
          ? `From ${providers.map((p) => p.name).join(" and ")}`
          : undefined
      }
      toolbar={
        providers.length > 1 ? (
          <PickerPill
            label="Provider"
            value={provider}
            onChange={setProvider}
            options={[
              { value: "all", label: "All" },
              ...providers.map((p) => ({ value: p.id, label: p.name })),
            ]}
          />
        ) : undefined
      }
    >
      {record.data ? (
        <RecordBody
          record={record.data}
          provider={provider}
          openKey={openKey}
          findings={findings.data ?? []}
          seenAt={seen.seenAt}
          onMarkAllRead={seen.markAllRead}
          onOpenFile={openFile}
          providers={providers}
        />
      ) : record.error ? (
        <div className="overflow-hidden rounded-[10px] bg-card">
          <ProblemBox
            role="alert"
            title="Your record didn't load"
            message={describe(record.error)}
            action={{ title: "Try Again", onClick: record.refresh }}
          />
        </div>
      ) : (
        <div
          className="overflow-hidden rounded-[10px] bg-card"
          aria-busy="true"
          aria-label="Loading your record"
        >
          <SkeletonRow hasAvatar={false} />
          <SkeletonRow hasAvatar={false} />
          <SkeletonRow hasAvatar={false} />
        </div>
      )}
    </Screen>
  )
}

function RecordBody(props: {
  record: ClinicalRecord
  provider: string
  openKey?: string
  findings: Finding[]
  seenAt?: string
  onMarkAllRead: () => Promise<void>
  onOpenFile?: OpenFile
  providers: { id: string; name: string }[]
}) {
  const { record, provider, openKey } = props
  const { today } = useMode()
  const now = today.getTime()
  const pick = (rows: ClinicalRow[]) =>
    provider === "all" ? rows : rows.filter((r) => r.provider === provider)
  const org = (r: ClinicalRow) => organizationName(record, r, props.providers)
  const unread = (r: ClinicalRow) => isUnread(r, props.seenAt, today)
  const isOpen = (r: ClinicalRow) => rowKey(r) === openKey

  const findings = sortFindings(
    props.findings.filter((f) => {
      if (provider === "all") return true
      const id = f.subject.slice(f.subject.indexOf(":") + 1)
      return id.split("/")[0] === provider || !f.subject.includes("/")
    })
  )
  const conditions = pick(record.rows.condition)
  const meds = pick(record.rows.medicationrequest)
  const plans = meds.filter(
    (m) => m.p.intent === "plan" || m.p.status === "active"
  )
  const pastMeds = meds.filter((m) => !plans.includes(m))
  const allergies = pick(record.rows.allergy)
  const vaccines = pick(record.rows.immunization)
  const appts = pick(record.rows.appointment)
  const upcoming = appts
    .filter((a) => a.at && Date.parse(a.at) >= now)
    .reverse()
  const past = appts.filter((a) => !upcoming.includes(a))
  const referrals = pick(record.rows.referral)
  const documents = pick(record.rows.document)
  const encounters = pick(record.rows.encounter)
  const messages = pick(record.rows.communication)
  const docs = useFirstTen(documents, isOpen)
  const consults = useFirstTen(encounters, isOpen)
  const msgs = useFirstTen(messages, isOpen)
  const unreadCount = [
    ...record.rows.document,
    ...record.rows.communication,
  ].filter(unread).length

  return (
    <>
      {findings.length > 0 && (
        <RecordSection
          id="attention"
          title="Needs attention"
          count={findings.length}
          action={
            unreadCount > 0 ? (
              <TextButton
                className="min-h-8 text-[13px]"
                onClick={() => void props.onMarkAllRead()}
              >
                Mark all read
              </TextButton>
            ) : undefined
          }
        >
          {findings.map((f) => (
            <FindingRow key={f.subject} finding={f} />
          ))}
        </RecordSection>
      )}
      <RecordSection
        id="conditions"
        title="Conditions"
        count={conditions.length}
        empty="No conditions"
      >
        {conditions.map((c) => (
          <RecordRow
            key={c.id}
            title={c.title}
            description={sentences(str(c.p.notes), org(c))}
            meta={
              <>
                {word(c.p.clinicalStatus) && (
                  <Badge
                    label={word(c.p.clinicalStatus)!}
                    tone={c.p.clinicalStatus === "active" ? "info" : "neutral"}
                  />
                )}
                <RowDate at={c.at} />
              </>
            }
          />
        ))}
      </RecordSection>
      <RecordSection
        id="medicines"
        title="Medicines"
        count={meds.length}
        empty="No current medicines"
        after={
          pastMeds.length > 0 && (
            <Disclosure
              className="mt-2 px-4"
              title="Past prescriptions"
              meta={pastMeds.length}
            >
              <RecordCard className="-mx-4">
                {pastMeds.map((m) => (
                  <RecordRow
                    key={m.id}
                    title={m.title}
                    description={sentences(str(m.p.dosage), org(m))}
                    meta={<RowDate at={m.at} />}
                  />
                ))}
              </RecordCard>
            </Disclosure>
          )
        }
      >
        {plans.map((m) => (
          <RecordRow
            key={m.id}
            title={m.title}
            description={sentences(
              str(m.p.dosage),
              str(m.p.nextIssueAt)
                ? `Next issue ${formatDate(m.p.nextIssueAt as string)}`
                : undefined,
              org(m)
            )}
            meta={
              <>
                {word(m.p.status) && (
                  <Badge label={word(m.p.status)!} tone="info" />
                )}
                <RowDate at={m.at} />
              </>
            }
          />
        ))}
      </RecordSection>
      <RecordSection
        id="allergies"
        title="Allergies"
        count={allergies.length}
        empty="No allergies recorded"
      >
        {allergies.map((a) => (
          <AllergyRow key={a.id} allergy={a} org={org(a)} />
        ))}
      </RecordSection>
      <RecordSection
        id="vaccinations"
        title="Vaccinations"
        count={vaccines.length}
        empty="No vaccinations"
      >
        {vaccines.map((v) => (
          <RecordRow
            key={v.id}
            title={v.title}
            description={org(v)}
            meta={<RowDate at={v.at} />}
          />
        ))}
      </RecordSection>
      <RecordSection
        id="appointments"
        title="Appointments"
        count={appts.length}
        empty="Nothing booked"
        after={
          past.length > 0 && (
            <Disclosure
              className="mt-2 px-4"
              title="Past appointments"
              meta={past.length}
            >
              <RecordCard className="-mx-4">
                {past.map((a) => (
                  <AppointmentRow
                    key={a.id}
                    appt={a}
                    org={org(a)}
                    upcoming={false}
                  />
                ))}
              </RecordCard>
            </Disclosure>
          )
        }
      >
        {upcoming.map((a) => (
          <AppointmentRow key={a.id} appt={a} org={org(a)} upcoming />
        ))}
      </RecordSection>
      <RecordSection
        id="referrals"
        title="Referrals"
        count={referrals.length}
        empty="No referrals"
      >
        {referrals.map((r) => (
          <ReferralRow key={r.id} referral={r} org={org(r)} />
        ))}
      </RecordSection>
      <RecordSection
        id="documents"
        title="Documents"
        count={documents.length}
        empty="No documents"
        after={docs.more}
      >
        {docs.shown.map((d) => (
          <DocumentRow
            key={d.id}
            doc={d}
            org={org(d)}
            isNew={unread(d)}
            isOpen={isOpen(d)}
            onOpenFile={props.onOpenFile}
          />
        ))}
      </RecordSection>
      <RecordSection
        id="consultations"
        title="Consultations"
        count={encounters.length}
        empty="No consultations"
        after={consults.more}
      >
        {consults.shown.map((e) => (
          <ConsultationRow
            key={e.id}
            encounter={e}
            org={org(e)}
            isOpen={isOpen(e)}
          />
        ))}
      </RecordSection>
      <RecordSection
        id="messages"
        title="Messages"
        count={messages.length}
        empty="No messages"
        after={msgs.more}
      >
        {msgs.shown.map((m) => (
          <MessageRow
            key={m.id}
            message={m}
            org={org(m)}
            isNew={unread(m)}
            isOpen={isOpen(m)}
            onOpenFile={props.onOpenFile}
          />
        ))}
      </RecordSection>
    </>
  )
}
