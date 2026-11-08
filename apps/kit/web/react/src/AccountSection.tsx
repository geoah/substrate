import { useState } from "react"
import { useSubstrate } from "./SubstrateApp"
import { ActionRow, Confirm, Row, Section } from "./ui"

function formatDate(ms: number): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(ms)
}

/** Settings' account sections, the same in every app: the server and
 * repository signed in to, what the approval gave, and signing out (the
 * kit's SettingsPresenter link rows). */
export function AccountSection() {
  const { session, signOut, manifest } = useSubstrate()
  const [asking, setAsking] = useState(false)
  const host = new URL(session.server).host
  return (
    <>
      <Section header="Repository">
        <Row label="Repository" value={session.repository} />
        <Row label="Server" value={host} />
        <Row
          label="Access ends"
          value={
            session.expiresAt
              ? formatDate(session.expiresAt)
              : "When you revoke it"
          }
        />
      </Section>
      <Section
        header="Access"
        footer={`What ${manifest.name} may do in your repository. Revoke it any time in your server's Settings, under Apps.`}
      >
        {session.scopes.map((s) => (
          <Row
            key={s}
            label={<span className="font-mono text-[15px] break-all">{s}</span>}
          />
        ))}
        {session.details
          .filter((d) => d.type === "package")
          .map((d) => (
            <Row
              key={d.package}
              label="Package"
              value={`${d.package} (version ${d.version})`}
            />
          ))}
      </Section>
      <Section>
        <ActionRow destructive onClick={() => setAsking(true)}>
          Sign Out
        </ActionRow>
      </Section>
      <Confirm
        open={asking}
        title={`Sign out of ${session.repository}?`}
        message={`${manifest.name} gives back its access. What it wrote stays in your repository.`}
        confirm="Sign Out"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false)
          void signOut()
        }}
      />
    </>
  )
}
