/** The choices a package row takes on the approval page, inside the row
 * itself: whether an optional add-on is included (decision 0216), and the
 * values of the settings and secrets the package needs (decision 0218).
 * Each is named by its label, with its id beside it, so a reader knows both
 * what the field is and which record it writes. A required field says so
 * to assistive technology and names itself once it is left empty. */

import { useId, useState } from "react"

import { Mono } from "@/components/authorize/parts"
import { Input } from "@/components/ui/input"
import type {
  AuthorizationPackage,
  AuthorizationSetting,
  AuthorizationSettingValue,
} from "@/lib/api/types"
import { packageName, settingLabel } from "@/lib/app-setup"

function settingKey(field: Pick<AuthorizationSetting, "id" | "secret">) {
  return `${field.secret ? "secret" : "setting"}/${field.id}`
}

function SettingField({
  field,
  value,
  disabled,
  onChange,
}: {
  field: AuthorizationSetting
  value: string
  disabled: boolean
  onChange: (value: string) => void
}) {
  const id = useId()
  const [touched, setTouched] = useState(false)
  const label = settingLabel(field.id)
  const missing = field.required && touched && value === ""
  const describedBy = [
    field.description ? `${id}-description` : "",
    missing ? `${id}-error` : "",
  ]
    .filter(Boolean)
    .join(" ")
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="font-medium">
        {label}
      </label>
      <span className="text-muted-foreground">
        {field.required ? "Required" : "Optional"}
        {field.secret ? ", kept sealed" : ""} · <Mono>{field.id}</Mono>
      </span>
      {field.description && (
        <span id={`${id}-description`} className="text-muted-foreground">
          {field.description}
        </span>
      )}
      <Input
        id={id}
        type={field.secret ? "password" : "text"}
        // "off" is ignored for a password field; "new-password" is what
        // every browser honours as "do not fill this in".
        autoComplete={field.secret ? "new-password" : "off"}
        disabled={disabled}
        value={value}
        aria-required={field.required}
        aria-invalid={missing || undefined}
        aria-describedby={describedBy || undefined}
        onBlur={() => setTouched(true)}
        onChange={(e) => onChange(e.target.value)}
      />
      {missing && (
        <span id={`${id}-error`} className="text-destructive">
          Fill in {label.toLowerCase()}.
        </span>
      )}
    </div>
  )
}

export function PackageChoices({
  pkg,
  waiting,
  selected,
  disabled,
  onSelect,
  values,
  onValues,
}: {
  pkg: AuthorizationPackage
  /** The add-on's provider is not here yet: approving records the choice
   * and the install waits for the provider. */
  waiting: boolean
  selected: boolean
  disabled: boolean
  onSelect: (checked: boolean) => void
  values: AuthorizationSettingValue[]
  onValues: (values: AuthorizationSettingValue[]) => void
}) {
  if (!pkg.addonFor && !pkg.settings?.length) return null
  return (
    <div className="flex flex-col gap-3 pt-1 text-xs">
      {pkg.addonFor && (
        <label className="flex items-start gap-2">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={selected}
            disabled={disabled}
            onChange={(e) => onSelect(e.target.checked)}
          />
          <span className="flex flex-col gap-0.5">
            <span className="font-medium [overflow-wrap:anywhere]">
              Include {packageName(pkg.id)}
            </span>
            <span className="text-muted-foreground">
              <Mono>{pkg.id}</Mono>
            </span>
            {waiting && (
              <span className="text-muted-foreground">
                Installs when you connect {packageName(pkg.addonFor)}.
              </span>
            )}
          </span>
        </label>
      )}
      {selected &&
        !waiting &&
        pkg.settings?.map((field) => (
          <SettingField
            key={settingKey(field)}
            field={field}
            value={
              values.find((v) => v.id === field.id && v.secret === field.secret)
                ?.value ?? ""
            }
            disabled={disabled}
            onChange={(value) =>
              onValues([
                ...values.filter(
                  (v) => v.id !== field.id || v.secret !== field.secret
                ),
                { id: field.id, secret: field.secret, value },
              ])
            }
          />
        ))}
    </div>
  )
}
