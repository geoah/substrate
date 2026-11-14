/** Publishing one of the repository's own packages, and writing the next
 * release of one it publishes. A package has a release; a pinned value is
 * released to sync, which is the marks' word and not this. A package in `~` offers Publish…: the owner names the
 * authority, a domain whose DNS they control, and the dialog states what
 * the publish does before it runs. The confirm waits until DNS shows the
 * authority's record and nothing stops the publish, because the server
 * refuses it otherwise. A package the repository publishes offers
 * New release…. Neither is offered by a server that does not list the
 * `publishing` feature. */

import { useState, type ReactNode } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { DnsRecord } from "@/components/dns-record"
import { Facts } from "@/components/identity/facts"
import { Pill } from "@/components/identity/pill"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { discoveryQueryOptions, hasFeature } from "@/lib/api/discovery"
import { LOCAL_AUTHORITY } from "@/lib/api/http"
import {
  PUBLISHING_FEATURE,
  planPublish,
  publish,
  release,
  releasesOf,
  releasesQueryOptions,
} from "@/lib/api/publish"
import type { PublishPlan, Release } from "@/lib/api/types"
import { relativeRecordName } from "@/lib/dns"

/** The `publishing` revision that added these routes. */
const PUBLISHING_REVISION = 1

/** Whether the server publishes packages. A console bundled in an app may
 * talk to an older one. */
function usePublishingOffered(): boolean {
  const discovery = useQuery(discoveryQueryOptions)
  return hasFeature(discovery.data, PUBLISHING_FEATURE, PUBLISHING_REVISION)
}

function count(n: number, one: string, many: string): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`
}

function when(at: string): string {
  const d = new Date(at)
  return Number.isNaN(d.getTime()) ? at : d.toLocaleString()
}

/** A reference, whole: wrapped rather than cut, since which package or kind
 * it names is the point. */
function Ref({ children }: { children: string }) {
  return <span className="data [overflow-wrap:anywhere]">{children}</span>
}

/** The server lowercases and trims the authority; the console compares what
 * was previewed with what is typed the same way. */
function normalizeAuthority(typed: string): string {
  return typed.trim().toLowerCase().replace(/\.$/, "")
}

/** The action a package's head carries: Publish… for one of the
 * repository's own, New release… for one it publishes, nothing otherwise. */
export function PackagePublishActions({
  authority,
  pkg,
  onPublished,
}: {
  authority: string
  pkg: string
  /** After the published dialog closes, with the package's new name. */
  onPublished?: (published: string) => void
}) {
  const offered = usePublishingOffered()
  const local = authority === LOCAL_AUTHORITY
  const all = useQuery({ ...releasesQueryOptions, enabled: offered && !local })
  const [open, setOpen] = useState(false)
  if (!offered) return null
  const id = `${authority}/${pkg}`
  if (local) {
    return (
      <>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          Publish…
        </Button>
        {open && (
          <PublishPackageDialog
            pkg={id}
            onPublished={onPublished}
            onClose={() => setOpen(false)}
          />
        )}
      </>
    )
  }
  const mine = releasesOf(all.data, id)
  const last = mine[mine.length - 1]
  if (!last) return null
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        New release…
      </Button>
      {open && (
        <ReleasePackageDialog last={last} onClose={() => setOpen(false)} />
      )}
    </>
  )
}

/** Publish `pkg`, a package in `~`: the authority, the plan, the DNS proof,
 * then the confirm. The plan is the server's, read again by Check again,
 * and stands only for the authority it was read for. */
function PublishPackageDialog({
  pkg,
  onPublished,
  onClose,
}: {
  /** `~/<package>`. */
  pkg: string
  onPublished?: (published: string) => void
  onClose: () => void
}) {
  const client = useQueryClient()
  const name = pkg.slice(pkg.indexOf("/") + 1)
  const [typed, setTyped] = useState("")
  const [planned, setPlanned] = useState<{
    authority: string
    plan: PublishPlan
  } | null>(null)
  const authority = normalizeAuthority(typed)
  const run = useMutation({
    mutationFn: (to: string) => publish(pkg, to),
  })
  const preview = useMutation({
    mutationFn: (to: string) => planPublish(pkg, to),
    onMutate: () => run.reset(),
    onSuccess: (plan, to) => setPlanned({ authority: to, plan }),
    onError: () => setPlanned(null),
  })
  const plan =
    planned && planned.authority === authority ? planned.plan : undefined
  const blockers = plan?.blockers ?? []
  const ready = Boolean(plan?.proven) && blockers.length === 0

  if (run.data) {
    const done = run.data
    return (
      <PublishedDialog
        plan={done}
        onClose={() => {
          // Only now: the package's collections leave `~`, and a list the
          // dialog sits in would unmount it before the release was read.
          void client.invalidateQueries()
          onPublished?.(done.published)
          onClose()
        }}
      />
    )
  }

  return (
    <ConfirmDialog
      className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg"
      title={
        <>
          Publish <Ref>{pkg}</Ref>?
        </>
      }
      consequence="Publishing gives this package a name under a domain you control, so other substrates can install it. Nothing changes until you confirm."
      confirm={plan ? `Publish as ${plan.published}` : "Publish"}
      pending={run.isPending}
      disabled={!ready}
      error={run.error?.message}
      onConfirm={() => run.mutate(authority)}
      onClose={onClose}
    >
      <form
        aria-label="Where to publish"
        onSubmit={(e) => {
          e.preventDefault()
          if (authority) preview.mutate(authority)
        }}
      >
        <Field>
          <FieldLabel htmlFor="publish-authority">Authority</FieldLabel>
          <div className="flex gap-2">
            <Input
              id="publish-authority"
              autoComplete="off"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              placeholder="ada.example.com"
              className="font-mono"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
            />
            <Button
              type="submit"
              variant="outline"
              disabled={!authority || preview.isPending}
            >
              {preview.isPending && !plan && <Spinner />}
              Preview
            </Button>
          </div>
          <FieldDescription>
            A domain whose DNS you control. The package is published as{" "}
            <Ref>{`${authority || "<authority>"}/${name}`}</Ref>.
          </FieldDescription>
        </Field>
      </form>
      {preview.isError && (
        <p role="alert" className="text-sm text-destructive">
          The preview didn’t load: {preview.error.message}
        </p>
      )}
      {plan && (
        <>
          <PlanFacts plan={plan} />
          {blockers.length > 0 && <Blockers blockers={blockers} />}
          <Proof
            plan={plan}
            authority={authority}
            checking={preview.isPending}
            onCheck={() => preview.mutate(authority)}
          />
          {!ready && (
            <p className="text-[12.5px] text-faint">
              {blockers.length > 0
                ? "Publishing waits until nothing stops it."
                : "Publishing waits until DNS shows the record."}
            </p>
          )}
        </>
      )}
    </ConfirmDialog>
  )
}

/** What the publish does, in the order a person weighs it: the name for
 * good, then what moves under it. */
function PlanFacts({ plan }: { plan: PublishPlan }) {
  const before = (kind: string) =>
    `${plan.package}/${kind.slice(plan.published.length + 1)}`
  const rows: Array<[string, ReactNode]> = [
    ["Package", <Ref key="from">{plan.package}</Ref>],
    ["Published as", <Ref key="to">{plan.published}</Ref>],
    [
      "Kinds that move",
      plan.kinds.length ? (
        <ul className="flex flex-col gap-1">
          {plan.kinds.map((k) => (
            <li key={k}>
              <Ref>{before(k)}</Ref> <span className="text-faint">→</span>{" "}
              <Ref>{k}</Ref>
            </li>
          ))}
        </ul>
      ) : (
        "None"
      ),
    ],
    [
      "Records that move",
      plan.records
        ? `${count(plan.records, "record", "records")}, each keeping its id and properties`
        : "None",
    ],
    [
      "References repointed",
      plan.references
        ? `${count(plan.references, "record", "records")} elsewhere point at them, and follow them to the new kinds`
        : "None",
    ],
    [
      "Your packages that follow",
      plan.packages.length ? (
        <span className="flex flex-col gap-1">
          <ul className="flex flex-col gap-1">
            {plan.packages.map((p) => (
              <li key={p}>
                <Ref>{p}</Ref>
              </li>
            ))}
          </ul>
          <span className="text-faint">
            Their definitions name this package’s kinds, and are rewritten to
            the new names.
          </span>
        </span>
      ) : (
        "None"
      ),
    ],
  ]
  return (
    <section aria-label="What publishing does" className="flex flex-col gap-2">
      <h3 className="text-[13px] font-medium">What publishing does</h3>
      <Facts rows={rows} />
      <p className="rounded-md bg-warn-soft px-3 py-2 text-[13px]">
        <span className="font-medium">This is permanent.</span>{" "}
        <Ref>{plan.package}</Ref> leaves <Ref>~</Ref> and keeps the name{" "}
        <Ref>{plan.published}</Ref> for good. It can’t be unpublished, or
        published again under another name.
      </p>
    </section>
  )
}

function Blockers({ blockers }: { blockers: string[] }) {
  return (
    <section aria-label="What stops it" className="flex flex-col gap-1.5">
      <h3 className="text-[13px] font-medium text-destructive">
        What stops it now
      </h3>
      <ul className="list-disc pl-5 text-[13px] [overflow-wrap:anywhere]">
        {blockers.map((b) => (
          <li key={b}>{b}</li>
        ))}
      </ul>
    </section>
  )
}

/** The authority's proof: the TXT record to publish, and whether DNS shows
 * it yet. */
function Proof({
  plan,
  authority,
  checking,
  onCheck,
}: {
  plan: PublishPlan
  authority: string
  checking: boolean
  onCheck: () => void
}) {
  const relative = plan.record ? relativeRecordName(plan.name) : null
  return (
    <section
      aria-label="Prove the authority"
      className="flex flex-col gap-2 text-[12.5px]"
    >
      <h3 className="text-[13px] font-medium">
        Prove <Ref>{authority}</Ref> is yours
      </h3>
      {plan.record && (
        <>
          <p>
            In the DNS settings for <Ref>{authority}</Ref>, add this TXT record,
            replacing any already at that name. Other substrates read it to find
            the package.
          </p>
          <DnsRecord name={plan.name} type="TXT" value={plan.record} />
          {relative && (
            <span className="text-faint">
              Some providers add the domain for you. If yours does, enter just{" "}
              <Ref>{relative.relative}</Ref> (your domain being{" "}
              <Ref>{relative.zone}</Ref>).
            </span>
          )}
        </>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-faint">DNS</span>
        {plan.proven ? (
          <Pill tone="ok">Record found</Pill>
        ) : (
          <Pill tone="warn">Not found yet</Pill>
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={checking}
          onClick={onCheck}
        >
          {checking && <Spinner />}
          Check again
        </Button>
      </div>
      {!plan.proven && plan.reason && (
        <p className="[overflow-wrap:anywhere] text-muted-foreground">
          {plan.reason}
        </p>
      )}
    </section>
  )
}

/** One release, labeled as the release dialogs show it. */
function ReleaseFacts({ release }: { release: Release }) {
  const rows: Array<[string, ReactNode]> = [
    ["Package", <Ref key="package">{release.package}</Ref>],
    ["Version", String(release.version)],
    ["SHA-256", <Ref key="sha">{release.sha256}</Ref>],
  ]
  if (release.dialect) rows.push(["Dialect", String(release.dialect)])
  rows.push(["Released", when(release.releasedAt)])
  return <Facts rows={rows} />
}

function PublishedDialog({
  plan,
  onClose,
}: {
  plan: PublishPlan
  onClose: () => void
}) {
  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            Published as <Ref>{plan.published}</Ref>
          </DialogTitle>
          <DialogDescription>
            Its first release is written, and other substrates can install it.
            Release it again after you change its definitions.
          </DialogDescription>
        </DialogHeader>
        {plan.release && <ReleaseFacts release={plan.release} />}
        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Write the next release of a package this repository publishes. The
 * server reads the authority's record again and refuses a release with
 * nothing changed; either refusal is its own words. */
function ReleasePackageDialog({
  last,
  onClose,
}: {
  /** The package's newest release. */
  last: Release
  onClose: () => void
}) {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: () => release(last.package),
    onSuccess: () =>
      client.invalidateQueries({ queryKey: releasesQueryOptions.queryKey }),
  })
  if (run.data) {
    return (
      <Dialog open onOpenChange={(next) => !next && onClose()}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Release {run.data.version} written</DialogTitle>
            <DialogDescription>
              Substrates that installed <Ref>{run.data.package}</Ref> can take
              it as an update.
            </DialogDescription>
          </DialogHeader>
          <ReleaseFacts release={run.data} />
          <DialogFooter>
            <Button onClick={onClose}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )
  }
  return (
    <ConfirmDialog
      className="sm:max-w-lg"
      title={
        <>
          Write the next release of <Ref>{last.package}</Ref>?
        </>
      }
      consequence={`This writes the package’s definitions as they are now as its next version, and substrates that installed it can take that version as an update. A package with nothing changed since version ${last.version} is refused.`}
      confirm="Write the next release"
      pending={run.isPending}
      error={run.error?.message}
      onConfirm={() => run.mutate()}
      onClose={onClose}
    >
      <Facts
        rows={[
          ["Package", <Ref key="package">{last.package}</Ref>],
          [
            "Last release",
            `Version ${last.version}, released ${when(last.releasedAt)}`,
          ],
        ]}
      />
    </ConfirmDialog>
  )
}
