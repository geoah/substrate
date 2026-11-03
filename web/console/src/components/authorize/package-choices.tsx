import type {
  AuthorizationPackage,
  AuthorizationSettingValue,
} from "@/lib/api/types"
import { Input } from "@/components/ui/input"

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
  waiting: boolean
  selected: boolean
  disabled: boolean
  onSelect: (checked: boolean) => void
  values: AuthorizationSettingValue[]
  onValues: (values: AuthorizationSettingValue[]) => void
}) {
  if (!pkg.addonFor && !pkg.settings?.length) return null
  return (
    <li className="flex flex-col gap-4 p-4">
      {pkg.addonFor && (
        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            checked={selected}
            disabled={disabled}
            onChange={(e) => onSelect(e.target.checked)}
          />
          <span className="[overflow-wrap:anywhere]">
            Include {pkg.id}
            {waiting && (
              <span className="block text-muted-foreground">
                Installs when you connect {pkg.addonFor}.
              </span>
            )}
          </span>
        </label>
      )}
      {selected &&
        !waiting &&
        pkg.settings?.map((field) => {
          const key = `${field.secret ? "secret" : "setting"}/${field.id}`
          const value =
            values.find((v) => v.id === field.id && v.secret === field.secret)
              ?.value ?? ""
          return (
            <label key={key} className="flex flex-col gap-2 text-sm">
              <span className="[overflow-wrap:anywhere]">
                {field.id}
                {field.required ? " (required)" : " (optional)"}
              </span>
              {field.description && (
                <span className="text-muted-foreground">
                  {field.description}
                </span>
              )}
              <Input
                type={field.secret ? "password" : "text"}
                autoComplete="off"
                disabled={disabled}
                value={value}
                onChange={(e) =>
                  onValues([
                    ...values.filter(
                      (v) => v.id !== field.id || v.secret !== field.secret
                    ),
                    {
                      id: field.id,
                      secret: field.secret,
                      value: e.target.value,
                    },
                  ])
                }
              />
            </label>
          )
        })}
    </li>
  )
}
