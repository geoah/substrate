/** The toolbar's Sort menu: which column the grid is sorted by and which way.
 * It edits the same sorting state a header click does, so the two never
 * disagree. */

import type { RowData } from "@tanstack/react-table"
import { ArrowDownUpIcon } from "lucide-react"

import type { DataTableInstance } from "@/components/data-table/data-table"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export function DataGridSort<TData extends RowData>({
  table,
}: {
  table: DataTableInstance<TData>
}) {
  const sorted = table.state.sorting[0]
  const columns = table
    .getAllLeafColumns()
    .filter((column) => column.getCanSort())
  const current = sorted ? table.getColumn(sorted.id) : undefined
  const label = current?.columnDef.meta?.label ?? sorted?.id
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            aria-label={label ? `Sorted by ${label}` : "Sort"}
            title={label ? `Sorted by ${label}` : "Sort"}
            className="text-muted-foreground"
          />
        }
      >
        <ArrowDownUpIcon className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-96 min-w-52">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Sort by</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={sorted?.id ?? ""}
            onValueChange={(id) =>
              table.setSorting([{ id: String(id), desc: sorted?.desc ?? true }])
            }
          >
            {columns.map((column) => (
              <DropdownMenuRadioItem key={column.id} value={column.id}>
                {column.columnDef.meta?.label ?? column.id}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        {sorted && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuRadioGroup
                value={sorted.desc ? "desc" : "asc"}
                onValueChange={(dir) =>
                  table.setSorting([{ id: sorted.id, desc: dir === "desc" }])
                }
              >
                <DropdownMenuRadioItem value="asc">
                  Ascending
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="desc">
                  Descending
                </DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
