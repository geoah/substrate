import { describe } from "@substrate/client"
import { BarButton, ProblemBox, Section, Spinner } from "@substrate/react"
import { useState } from "react"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import type { Owner, ProfileSave } from "../data/profile"
import type { Profile, ProfileDraft, Sex } from "../kinds"
import { formatDate } from "../lib/format"
import { birthDateProblem, birthDay, localDay } from "../profile/owner"
import { go } from "../route"
import { TextButton } from "../ui/IconButton"
import { Screen } from "../ui/Screen"
import {
  BasisField,
  CONSOLE,
  ConflictChoice,
  ConsoleLink,
  DateField,
  SEX_WORDS,
  SourceRows,
  Suggestion,
  ValueRow,
} from "../ui/profile/parts"

/** The first open: who the owner is, what was found about them, a field
 * for each value still missing, and where data comes from. Finish and
 * Skip both record the start, so it opens once; Settings opens it again. */
export function GettingStartedScreen() {
  const owner = useBackend().profile.useOwner()
  if (owner.data)
    return (
      <GettingStarted
        owner={owner.data}
        contactProblem={owner.error ? describe(owner.error) : undefined}
        retry={owner.refresh}
      />
    )
  return (
    <Screen title="Getting started" noSettings>
      {owner.error ? (
        <div className="overflow-hidden rounded-[10px] bg-card">
          <ProblemBox
            role="alert"
            title="Your details didn't load"
            message={describe(owner.error)}
            action={{ title: "Try Again", onClick: owner.refresh }}
          />
        </div>
      ) : (
        <div className="flex justify-center py-16 text-secondary">
          <Spinner />
        </div>
      )}
    </Screen>
  )
}

function GettingStarted({
  owner,
  contactProblem,
  retry,
}: {
  owner: Owner
  /** Why the owner's contact could not be read, where it could not. */
  contactProblem?: string
  retry: () => void
}) {
  const backend = useBackend()
  const writes = backend.profile.useProfileWrites()
  const consoleLink = backend.profile.useConsoleLink()
  const sources = backend.sources.useSourceList()
  const { today, demo } = useMode()
  // What the form was opened on: a save holds to it.
  const [seenVersion] = useState(owner.profile?.version ?? 0)
  const started = !!owner.profile?.startedAt
  const askBirthDate = owner.birthDate?.origin !== "contact"
  const [birthDate, setBirthDate] = useState(
    owner.birthDate?.origin === "profile" ? owner.birthDate.value : ""
  )
  const [sex, setSex] = useState<Sex | undefined>(owner.sex?.value)
  const [problem, setProblem] = useState<string>()
  const [tried, setTried] = useState(false)
  const [busy, setBusy] = useState(false)
  const [conflict, setConflict] = useState<Profile>()

  const typed = birthDate.trim()
  const dateProblem = typed ? birthDateProblem(typed, today) : undefined
  const name = owner.contact?.name
  const suggested = owner.suggestions

  const draft = (): Partial<ProfileDraft> => {
    const d: Partial<ProfileDraft> = { startedAt: new Date().toISOString() }
    if (askBirthDate) {
      if (typed) d.birthDate = typed
      else if (owner.birthDate?.origin === "profile") d.birthDate = null
    }
    if (sex) d.sex = sex
    return d
  }

  const done = (out: ProfileSave) => {
    setBusy(false)
    if (out.state === "saved") {
      setConflict(undefined)
      go(started ? { screen: "settings" } : { screen: "today" }, !started)
    } else if (out.state === "conflict") setConflict(out.theirs)
    else setProblem(out.words)
  }

  const save = async (d: Partial<ProfileDraft>, version = seenVersion) => {
    setBusy(true)
    setProblem(undefined)
    done(await writes.save(d, { seenVersion: version }))
  }

  const finish = () => {
    setTried(true)
    if (dateProblem) return
    void save(draft())
  }

  const skip = () => void save({ startedAt: new Date().toISOString() })

  return (
    <Screen
      title="Getting started"
      noSettings
      back={
        started
          ? { label: "Settings", onClick: () => go({ screen: "settings" }) }
          : undefined
      }
    >
      <p className="mb-6 px-4 text-[17px]">
        {name ? `Hello, ${name}. ` : "Hello. "}
        Healthspan reads your labs, sleep and training from your Substrate and
        keeps what no other record holds in a profile of its own. A few details
        first; you can change them later in Settings.
      </p>

      <Section header="About you">
        {owner.contact ? (
          <ValueRow
            label="Your contact"
            value={name ?? "No name"}
            note="The contact your Substrate marks as you"
          />
        ) : contactProblem ? (
          <ProblemBox
            title="Your contact couldn't be read"
            message={contactProblem}
            action={{ title: "Try Again", onClick: retry }}
          />
        ) : (
          <div className="px-4 py-2.5">
            <p className="text-[17px]">No contact is marked as you yet</p>
            <p className="mt-0.5 text-[15px] text-secondary">
              Your Substrate keeps your name as a contact of your own, which
              apps read to know who you are.{" "}
              <ConsoleLink link={consoleLink} path={CONSOLE.ownerName}>
                Set your name in the console
              </ConsoleLink>
            </p>
          </div>
        )}
        {askBirthDate ? (
          <DateField
            label="Birth date"
            value={birthDate}
            onChange={setBirthDate}
            max={localDay(today)}
            problem={tried ? dateProblem : undefined}
            help={
              owner.contact ? (
                <>
                  {owner.contact.birthday && !owner.contact.birthday.year
                    ? "Your contact has a birthday without a year. "
                    : "Your contact has no birthday. "}
                  This one is kept in Healthspan&rsquo;s profile;{" "}
                  <ConsoleLink
                    link={consoleLink}
                    path={CONSOLE.contact(owner.contact.id)}
                  >
                    add it to your contact
                  </ConsoleLink>{" "}
                  to share it with your other apps.
                </>
              ) : (
                "Kept in Healthspan's profile. The age score needs it."
              )
            }
          />
        ) : (
          <ValueRow
            label="Birth date"
            value={formatDate(birthDay(owner.birthDate!.value))}
            note="From your contact"
          />
        )}
        {askBirthDate &&
          suggested.birthDate &&
          suggested.birthDate !== typed && (
            <Suggestion
              owner={owner}
              what={`the birth date ${formatDate(birthDay(suggested.birthDate))}`}
              onUse={() => setBirthDate(suggested.birthDate!)}
            />
          )}
        <BasisField value={sex} onChange={setSex} />
        {suggested.sex && suggested.sex !== sex && (
          <Suggestion
            owner={owner}
            what={`${SEX_WORDS[suggested.sex].toLowerCase()} as your gender`}
            onUse={() => setSex(suggested.sex)}
          />
        )}
      </Section>

      <Section
        header="Where your data comes from"
        footer={
          <>
            Providers such as WHOOP, and Apple Health through Substrate Sync,
            are connected in{" "}
            <ConsoleLink link={consoleLink} path={CONSOLE.providers}>
              the console
            </ConsoleLink>
            . A lab report no provider delivers can be imported from a file.
          </>
        }
      >
        <SourceRows sources={sources} />
        <a
          href="#/sources/import"
          className="flex min-h-11 items-center px-4 py-2.5 text-[17px] text-accent-text active:bg-selected"
        >
          Import a lab report
        </a>
        <a
          href="#/sources"
          className="flex min-h-11 items-center px-4 py-2.5 text-[17px] text-accent-text active:bg-selected"
        >
          See every source
        </a>
      </Section>

      {!demo && (
        <p className="mb-6 px-4 text-[13px] text-secondary">
          To find the contact that is you, Healthspan reads the settings records
          of your Substrate, which name it. It can read every setting record
          that way; none of them holds a secret.
        </p>
      )}

      {(conflict || problem) && (
        <div className="mb-6 overflow-hidden rounded-[10px] bg-card">
          {conflict ? (
            <ConflictChoice
              mine={{ birthDate: draft().birthDate, sex }}
              theirs={conflict}
              busy={busy}
              onKeepMine={() => void save(draft(), conflict.version)}
              onUseTheirs={() =>
                void save(
                  conflict.startedAt
                    ? {}
                    : { startedAt: new Date().toISOString() },
                  conflict.version
                )
              }
            />
          ) : (
            <ProblemBox
              role="alert"
              title="Not saved"
              message={problem}
              action={{ title: "Try Again", onClick: finish, disabled: busy }}
            />
          )}
        </div>
      )}

      <div className="flex flex-wrap items-center justify-end gap-3 px-4">
        {!started && (
          <TextButton onClick={skip} disabled={busy}>
            Skip for now
          </TextButton>
        )}
        <BarButton
          prominent
          onClick={finish}
          disabled={busy || !!conflict}
          className="px-5"
        >
          {busy ? "Saving…" : started ? "Save" : "Finish"}
        </BarButton>
      </div>
    </Screen>
  )
}
