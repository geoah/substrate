/** The record's DATA, read as data (#38).
 *
 * The manifest tab is a YAML dump: correct, complete, and the wrong shape for
 * "what does this record say". A long text property is one folded scalar among
 * forty lines of indentation; a datetime is an ISO string; a pointer is a path
 * you cannot click. This is the same content laid out per datatype — one row
 * per DECLARED property, in the kind's own vocabulary, so the declaration is
 * what decides how a value reads rather than YAML's escaping rules.
 *
 * It is READ-ONLY on purpose, and deliberately not `PropertyField` with the
 * inputs disabled. That component's job is a form: it fetches picker options,
 * holds edit state and owns validation, none of which a reader needs, and a
 * greyed-out input reads as "you may not edit this" rather than "this is the
 * value". Editing has its own route — the Edit action, already on the header.
 *
 * Unset properties are shown, dimmed. A record is easier to read against the
 * shape of its kind than as the subset of keys that happen to hold something,
 * and "this field is empty" is an answer people come here for.
 */

import { Link } from "@tanstack/react-router"

import { CodeBlock } from "@/components/code-block"
import { StateBadge } from "@/components/state-badge"
import { Badge } from "@/components/ui/badge"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { splitKind } from "@/lib/api/http"
import {
  declaredEdges,
  declaredProperties,
  kindByIdentity,
  propertyTypeLabel,
  type DeclaredProperty,
} from "@/lib/definition"
import { humanizeName } from "@/lib/record-schema"
import { splitRecordPath } from "@/lib/record-path"
import { relativeTime, shortDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"

export function PropertiesRail({
  record,
  kind,
  kinds,
}: {
  record: SubstrateRecord
  /** The record's own kind, when the registry knows it. Without it there is no
   * declaration to read against, so the view falls back to whatever keys the
   * record carries. */
  kind?: KindInfo
  kinds: KindInfo[]
}) {
  const declared = kind ? declaredProperties(kind) : []
  const edges = kind ? declaredEdges(kind) : []

  // Anything the record carries that the declaration does not name. Normally
  // empty; when it is not, hiding it would make this view lie about "all its
  // data", so it gets a section that says what it is.
  const named = new Set(declared.map((p) => p.name))
  const undeclared = Object.keys(record.properties ?? {})
    .filter((k) => !named.has(k))
    .sort()

  if (!declared.length && !undeclared.length && !edges.length) {
    return (
      <p className="px-6 py-8 text-sm text-muted-foreground">
        This record declares no properties.
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-6 px-6 py-5">
      {declared.length > 0 && (
        <PropertyList>
          {declared.map((p) => (
            <PropertyRow
              key={p.name}
              property={p}
              value={record.properties?.[p.name]}
              kinds={kinds}
            />
          ))}
        </PropertyList>
      )}

      {undeclared.length > 0 && (
        <Section
          title="Not declared"
          note="Stored on the record, but the kind does not declare it — a leftover from an older declaration, or a write that predates one."
        >
          <PropertyList>
            {undeclared.map((name) => (
              <PropertyRow
                key={name}
                property={{ name, kind: "string", repeated: false }}
                value={record.properties?.[name]}
                kinds={kinds}
              />
            ))}
          </PropertyList>
        </Section>
      )}

      {edges.length > 0 && (
        <Section title="Edges">
          <PropertyList>
            {edges.map((e) => {
              const targets = record.edges?.[e.rel] ?? []
              return (
                <Row
                  key={e.rel}
                  name={e.rel}
                  type={e.many ? `${e.to}[]` : e.to}
                  description={e.description}
                >
                  {targets.length === 0 ? (
                    <Unset />
                  ) : (
                    <div className="flex flex-wrap gap-1.5">
                      {targets.map((t) => (
                        <RecordLink
                          key={`${t.kind}/${t.id}`}
                          kinds={kinds}
                          kind={t.kind}
                          id={t.id}
                          label={t.title || t.id}
                        />
                      ))}
                    </div>
                  )}
                </Row>
              )
            })}
          </PropertyList>
        </Section>
      )}
    </div>
  )
}

function Section({
  title,
  note,
  children,
}: {
  title: string
  note?: string
  children: React.ReactNode
}) {
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
        {title}
      </h2>
      {note && <p className="text-xs text-muted-foreground">{note}</p>}
      {children}
    </section>
  )
}

function PropertyList({ children }: { children: React.ReactNode }) {
  return <dl className="flex flex-col divide-y">{children}</dl>
}

/** One property: its name and datatype on the left, its value on the right,
 * stacking to one column when there is no room for two. */
function Row({
  name,
  type,
  description,
  children,
}: {
  name: string
  type: string
  description?: string
  children: React.ReactNode
}) {
  return (
    <div className="grid gap-1 py-2.5 sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)] sm:gap-4">
      <dt className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{humanizeName(name)}</span>
        <span className="data text-xs text-muted-foreground">{type}</span>
        {description && (
          <span className="text-xs text-muted-foreground">{description}</span>
        )}
      </dt>
      <dd className="min-w-0 text-sm">{children}</dd>
    </div>
  )
}

function PropertyRow({
  property,
  value,
  kinds,
}: {
  property: DeclaredProperty
  value: unknown
  kinds: KindInfo[]
}) {
  return (
    <Row
      name={property.name}
      type={propertyTypeLabel(property)}
      description={property.description}
    >
      <PropertyValue property={property} value={value} kinds={kinds} />
    </Row>
  )
}

/** The one place a datatype decides how a stored value READS. The editing
 * twin of this switch is `PropertyField`; the two are separate because a form
 * control and a rendered value want different things from the same
 * declaration. */
function PropertyValue({
  property,
  value,
  kinds,
}: {
  property: DeclaredProperty
  value: unknown
  kinds: KindInfo[]
}) {
  // A secret's stored value is sealed and the read is redacted, so there is
  // never anything here to show — say that rather than showing an empty slot,
  // which would read as "unset".
  if (property.kind === "secret") {
    return (
      <span className="text-muted-foreground italic">
        write-only — the read never serves it
      </span>
    )
  }
  if (value === null || value === undefined || value === "") return <Unset />

  if (property.repeated && Array.isArray(value)) {
    if (value.length === 0) return <Unset />
    return (
      <div className="flex flex-col gap-1.5">
        {value.map((v, i) => (
          <ScalarValue key={i} property={property} value={v} kinds={kinds} />
        ))}
      </div>
    )
  }
  return <ScalarValue property={property} value={value} kinds={kinds} />
}

function ScalarValue({
  property,
  value,
  kinds,
}: {
  property: DeclaredProperty
  value: unknown
  kinds: KindInfo[]
}) {
  switch (property.kind) {
    case "state":
      return <StateBadge value={String(value)} initial={property.initial} />

    case "enum": {
      // The declaration separates the stored value from its display label;
      // show the label and keep the raw value legible beside it.
      const match = property.values?.find((v) => v.value === String(value))
      const label = match?.label || humanizeName(String(value))
      return (
        <span className="flex flex-wrap items-baseline gap-1.5">
          <Badge variant="secondary">{label}</Badge>
          {label !== String(value) && (
            <span className="data text-xs text-muted-foreground">
              {String(value)}
            </span>
          )}
        </span>
      )
    }

    case "bool":
    case "boolean":
      return <Badge variant="secondary">{value ? "Yes" : "No"}</Badge>

    case "datetime":
    case "date": {
      const iso = String(value)
      const when = shortDateTime(iso)
      // shortDateTime hands the raw string back when it will not parse. The
      // stored value is the fact, so show it rather than "Invalid Date" — and
      // skip the relative line, which would be meaningless.
      if (when === iso) return <Plain value={iso} />
      return (
        <span className="flex flex-wrap items-baseline gap-1.5">
          <span>{when}</span>
          <span className="text-xs text-muted-foreground">
            {relativeTime(iso)}
          </span>
        </span>
      )
    }

    case "int":
    case "float":
    case "number":
      return <span className="data tabular-nums">{String(value)}</span>

    case "digest":
      return (
        <span className="data break-all text-muted-foreground">
          {String(value)}
        </span>
      )

    case "url": {
      const href = String(value)
      return (
        <a
          className="data break-all text-primary underline-offset-2 hover:underline"
          href={href}
          target="_blank"
          rel="noreferrer noopener"
        >
          {href}
        </a>
      )
    }

    case "email":
      return (
        <a
          className="data break-all text-primary underline-offset-2 hover:underline"
          href={`mailto:${String(value)}`}
        >
          {String(value)}
        </a>
      )

    case "reference":
      return <Reference property={property} value={value} kinds={kinds} />

    case "json":
      return (
        <CodeBlock
          source={JSON.stringify(value, null, 2)}
          lang="json"
          className="max-h-96 overflow-auto rounded border"
        />
      )

    case "text":
    case "markdown":
      // The "large text field" the ticket asks for: prose keeps its line
      // breaks and gets room, rather than being squeezed onto one line.
      return <p className="max-w-prose whitespace-pre-wrap">{String(value)}</p>

    default:
      // An object or map arrived under a datatype with no richer rendering:
      // JSON is honest and readable, and beats "[object Object]".
      if (typeof value === "object") {
        return (
          <CodeBlock
            source={JSON.stringify(value, null, 2)}
            lang="json"
            className="max-h-96 overflow-auto rounded border"
          />
        )
      }
      return <Plain value={String(value)} />
  }
}

/** A pointer: the flat `<kind>/<id>` path the reference model stores, rendered
 * as a link to the record it names.
 *
 * The path carries its own kind, so the split needs no registry. A value that
 * is not a path is shown as-is — an unresolvable pointer is still the stored
 * fact, and inventing a link for it would be worse than showing the string. */
function Reference({
  property,
  value,
  kinds,
}: {
  property: DeclaredProperty
  value: unknown
  kinds: KindInfo[]
}) {
  const raw = String(value)
  const parts = splitRecordPath(raw)
  if (!parts) return <Plain value={raw} />
  return (
    <RecordLink
      kinds={kinds}
      kind={parts.kind || property.to || ""}
      id={parts.id}
      label={parts.id}
    />
  )
}

function RecordLink({
  kinds,
  kind,
  id,
  label,
}: {
  kinds: KindInfo[]
  kind: string
  id: string
  label: string
}) {
  const info = kindByIdentity(kinds, kind)
  // An unroutable target (a kind this repository does not hold) is still a
  // fact worth showing — as text, since there is nowhere to go.
  if (!info) return <span className="data">{label}</span>
  const { authority } = splitKind(kind)
  return (
    <Link
      className="data text-primary underline-offset-2 hover:underline"
      to="/data/$authority/$plural/$id"
      params={{ authority, plural: info.plural, id }}
    >
      {label}
    </Link>
  )
}

function Plain({ value }: { value: string }) {
  return <span className="break-words">{value}</span>
}

function Unset() {
  return (
    <span className={cn("text-muted-foreground", "select-none")} title="unset">
      —
    </span>
  )
}
