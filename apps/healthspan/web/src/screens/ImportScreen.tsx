import { ProblemBox, Section, Spinner } from "@substrate/react"
import { describe } from "@substrate/client"
import { useBackend } from "../data"
import { go } from "../route"
import { EmptyState } from "../ui/EmptyState"
import { Screen } from "../ui/Screen"
import { ImportDetail } from "../ui/import/ImportDetail"
import { ImportWizard } from "../ui/import/ImportWizard"

const back = { label: "Sources", onClick: () => go({ screen: "sources" }) }

/** A new import from a file (docs/plans/healthspan.md, Import), or one
 * import with its state, its plan, Resume and Undo. */
export function ImportScreen(props: { id?: string }) {
  if (!props.id)
    return (
      <Screen title="Import" back={back}>
        <ImportWizard />
      </Screen>
    )
  return <OneImport id={props.id} />
}

function OneImport(props: { id: string }) {
  const read = useBackend().sources.useImport(props.id)
  const imp = read.data
  return (
    <Screen title={imp?.name ?? "Import"} back={back}>
      {imp ? (
        <ImportDetail imp={imp} />
      ) : read.error ? (
        <Section>
          <ProblemBox
            role="alert"
            title="Couldn't read this import"
            message={describe(read.error)}
            action={{ title: "Try Again", onClick: read.refresh }}
          />
        </Section>
      ) : imp === null ? (
        <EmptyState
          title="Not in your repository"
          description="This import may have been deleted."
          action={{ label: "All sources", onClick: back.onClick }}
        />
      ) : (
        <div className="flex justify-center py-16 text-secondary">
          <Spinner />
        </div>
      )}
    </Screen>
  )
}
