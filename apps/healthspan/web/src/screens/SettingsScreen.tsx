import { describe } from "@substrate/client"
import {
  ActionRow,
  BarButton,
  NoteRow,
  ProblemBox,
  Section,
  SubstrateSettings,
} from "@substrate/react"
import { useState } from "react"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import type { Owner, ProfileSave } from "../data/profile"
import { KIND, type Profile, type ProfileDraft, type Sex } from "../kinds"
import { useDisplayControl } from "../lib/display"
import { formatDate } from "../lib/format"
import { birthDateProblem, birthDay, localDay } from "../profile/owner"
import { go } from "../route"
import { DisplaySettings } from "../ui/DisplaySettings"
import { Screen } from "../ui/Screen"
import { SkeletonRow } from "../ui/Skeleton"
import {
  BasisField,
  CONSOLE,
  ConflictChoice,
  ConsoleLink,
  DateField,
  SEX_WORDS,
  Suggestion,
  ValueRow,
  birthDateNote,
} from "../ui/profile/parts"

/** Settings: the profile (each value with where it came from, the app's
 * own editable here), getting started again, the display settings of both
 * card screens, then the kit's sections. */
export function SettingsScreen() {
  const backend = useBackend()
  const owner = backend.profile.useOwner()
  const control = useDisplayControl()
  const { demo } = useMode()
  return (
    <Screen title="Settings" noSettings>
      {owner.data ? (
        <ProfileSettings
          owner={owner.data}
          contactProblem={owner.error ? describe(owner.error) : undefined}
          retry={owner.refresh}
        />
      ) : owner.error ? (
        <Section header="Profile">
          <ProblemBox
            role="alert"
            title="Your details didn't load"
            message={describe(owner.error)}
            action={{ title: "Try Again", onClick: owner.refresh }}
          />
        </Section>
      ) : (
        <Section header="Profile">
          <SkeletonRow hasAvatar={false} />
          <SkeletonRow hasAvatar={false} />
        </Section>
      )}
      <Section>
        <ActionRow onClick={() => go({ screen: "start" })}>
          Open Getting Started
        </ActionRow>
      </Section>

      <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
        Labs display
      </h2>
      <div className="mb-7">
        <DisplaySettings page="labs" control={control} variant="inline" />
      </div>
      <h2 className="mb-1.5 px-4 text-[13px] tracking-wide text-secondary uppercase">
        Healthspan display
      </h2>
      <div className="mb-7">
        <DisplaySettings page="healthspan" control={control} variant="inline" />
      </div>

      {demo ? (
        <Section header="Your Substrate">
          <NoteRow>
            The demo keeps nothing in a Substrate: its member is invented and
            made in this browser, and a change to the profile is gone when the
            page closes.
          </NoteRow>
        </Section>
      ) : (
        <>
          <Section
            header="What Healthspan reads"
            footer="Your display settings are kept in this browser; the profile above, in your Substrate."
          >
            <NoteRow>
              Healthspan reads the settings records of your Substrate to find
              the one that names your contact. It can read every setting record
              that way; none of them holds a secret.
            </NoteRow>
          </Section>
          <SubstrateSettings
            kindLabels={{
              [KIND.profile]: "Healthspan profile",
              [KIND.import]: "Import",
            }}
          />
        </>
      )}
    </Screen>
  )
}

function ProfileSettings({
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
  const { today } = useMode()
  const fromContact = owner.birthDate?.origin === "contact"
  const saved = {
    birthDate:
      owner.birthDate?.origin === "profile" ? owner.birthDate.value : "",
    sex: owner.sex?.value,
  }
  // The form holds what the owner changes, over the profile as it stands;
  // a save is an edit on the version it was opened on.
  const [edit, setEdit] = useState<{
    birthDate: string
    sex: Sex | undefined
    version: number
  }>()
  const [result, setResult] = useState<ProfileSave>()
  const [busy, setBusy] = useState(false)
  const [tried, setTried] = useState(false)
  const birthDate = edit?.birthDate ?? saved.birthDate
  const sex = edit ? edit.sex : saved.sex
  const change = (
    patch: Partial<{ birthDate: string; sex: Sex | undefined }>
  ) => {
    setResult(undefined)
    setEdit({
      birthDate,
      sex,
      version: edit?.version ?? owner.profile?.version ?? 0,
      ...patch,
    })
  }
  const typed = birthDate.trim()
  const problem = typed ? birthDateProblem(typed, today) : undefined
  const dirty = !!edit && (typed !== saved.birthDate || sex !== saved.sex)

  const draft = (): Partial<ProfileDraft> => {
    const d: Partial<ProfileDraft> = {}
    if (!fromContact && typed !== saved.birthDate) d.birthDate = typed || null
    if (sex !== saved.sex) d.sex = sex ?? null
    return d
  }
  const save = async (d: Partial<ProfileDraft>, version: number) => {
    setBusy(true)
    const out = await writes.save(d, { seenVersion: version })
    setBusy(false)
    setResult(out)
    if (out.state === "saved") {
      setEdit(undefined)
      setTried(false)
    }
  }
  const submit = () => {
    setTried(true)
    if (problem || !edit) return
    void save(draft(), edit.version)
  }

  return (
    <>
      <Section header="Profile">
        {owner.contact ? (
          <ValueRow
            label="Your contact"
            value={owner.contact.name ?? "No name"}
            note={
              consoleLink && (
                <ConsoleLink
                  link={consoleLink}
                  path={CONSOLE.contact(owner.contact.id)}
                >
                  Open it in the console
                </ConsoleLink>
              )
            }
          />
        ) : contactProblem ? (
          <ProblemBox
            title="Your contact couldn't be read"
            message={contactProblem}
            action={{ title: "Try Again", onClick: retry }}
          />
        ) : (
          <ValueRow
            label="Your contact"
            note={
              <>
                No contact is marked as you.{" "}
                <ConsoleLink link={consoleLink} path={CONSOLE.ownerName}>
                  Set your name in the console
                </ConsoleLink>
              </>
            }
          />
        )}
        {fromContact ? (
          <ValueRow
            label="Birth date"
            value={formatDate(birthDay(owner.birthDate!.value))}
            note={
              <>
                From your contact.{" "}
                {owner.contact && (
                  <ConsoleLink
                    link={consoleLink}
                    path={CONSOLE.contact(owner.contact.id)}
                    optional
                  >
                    Change it in the console
                  </ConsoleLink>
                )}
              </>
            }
          />
        ) : (
          <DateField
            label="Birth date"
            value={birthDate}
            onChange={(v) => change({ birthDate: v })}
            max={localDay(today)}
            problem={tried ? problem : undefined}
            help={birthDateNote(owner) ?? "Not set. The age score needs it."}
          />
        )}
        {!fromContact &&
          owner.suggestions.birthDate &&
          owner.suggestions.birthDate !== typed && (
            <Suggestion
              owner={owner}
              what={`the birth date ${formatDate(birthDay(owner.suggestions.birthDate))}`}
              onUse={() => change({ birthDate: owner.suggestions.birthDate })}
            />
          )}
        <BasisField
          value={sex}
          onChange={(v) => change({ sex: v })}
          clearable
          note={
            owner.sex
              ? "Kept by Healthspan"
              : sex
                ? undefined
                : "Not set: the age score and the lab targets need it"
          }
        />
        {owner.suggestions.sex && owner.suggestions.sex !== sex && (
          <Suggestion
            owner={owner}
            what={`${SEX_WORDS[owner.suggestions.sex].toLowerCase()} as your gender`}
            onUse={() => change({ sex: owner.suggestions.sex })}
          />
        )}
        {result?.state === "conflict" ? (
          <ConflictChoice
            mine={{ birthDate: draft().birthDate, sex }}
            theirs={result.theirs}
            busy={busy}
            onKeepMine={() => void save(draft(), result.theirs.version)}
            onUseTheirs={() => {
              setEdit(undefined)
              setResult(undefined)
            }}
          />
        ) : result?.state === "failed" ? (
          <ProblemBox
            role="alert"
            title="Not saved"
            message={result.words}
            action={{ title: "Try Again", onClick: submit, disabled: busy }}
          />
        ) : null}
        <div className="flex flex-wrap items-center justify-end gap-3 px-4 py-2.5">
          {result?.state === "saved" && !dirty && (
            <span role="status" className="text-[15px] text-secondary">
              Saved
            </span>
          )}
          {dirty && (
            <BarButton
              onClick={() => {
                setEdit(undefined)
                setResult(undefined)
                setTried(false)
              }}
              disabled={busy}
            >
              Cancel
            </BarButton>
          )}
          <BarButton
            prominent
            onClick={submit}
            disabled={!dirty || busy || result?.state === "conflict"}
            className="px-5"
          >
            {busy ? "Saving…" : "Save"}
          </BarButton>
        </div>
      </Section>
      <ProfileFootnote profile={owner.profile} />
    </>
  )
}

function ProfileFootnote({ profile }: { profile?: Profile }) {
  if (profile) return null
  return (
    <p className="-mt-5 mb-7 px-4 text-[13px] text-secondary">
      Healthspan keeps no profile yet: saving makes one in your Substrate.
    </p>
  )
}
