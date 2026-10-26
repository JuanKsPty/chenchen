/**
 * <OptionFilter label title value groups onPick onClear desktop />
 *
 *   label    the chip's name when off: "Cuenta", "Categoría", "Tipo"
 *   title    the sheet's title on a phone ("Filtrar por cuenta")
 *   value    the chosen value in words ("Tarjeta ···1111"), or undefined when off
 *   groups   the choices: [{ title?, options: [{ value, label, icon?, detail?,
 *            pending? }] }]
 *   onPick   called with the chosen option's value
 *   onClear  removes the filter (the whole active chip does it)
 *   desktop  from lg: a menu under the chip; below it, a bottom sheet with 52px rows
 *
 * Off, the chip opens the choices; on, it shows the value and clears it. Focus stays
 * on the chip through both, although it is a different element in each state.
 */
import type { LucideIcon } from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'

import { FilterChip } from '@/components/kit'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'
import { PickerGroupTitle, PickerRow, PickerSheet } from './PickerSheet'

export interface FilterOption {
  value: string
  label: string
  icon?: LucideIcon
  /** Muted text at the right: an account number, a range. */
  detail?: ReactNode
  /** "Sin categoría": the icon in primary-text. */
  pending?: boolean
}

export interface FilterOptionGroup {
  title?: string
  options: FilterOption[]
}

/** Puts focus back on the chip after it changes between off and on. */
export function useChipFocus(value: string | undefined) {
  const chip = useRef<HTMLButtonElement>(null)
  const pending = useRef(false)

  useEffect(() => {
    if (!pending.current) return
    pending.current = false
    chip.current?.focus()
  }, [value])

  return {
    chip,
    /** Call before clearing: the off chip that replaces it gets the focus. */
    focusAfterChange: () => {
      pending.current = true
    },
    /** For the menu or sheet: return to whichever chip is showing when it closes. */
    onCloseAutoFocus: (event: Event) => {
      event.preventDefault()
      chip.current?.focus()
    },
  }
}

export function OptionFilter({
  label,
  title,
  value,
  groups,
  onPick,
  onClear,
  desktop,
}: {
  label: string
  title: string
  value?: string
  groups: FilterOptionGroup[]
  onPick: (value: string) => void
  onClear: () => void
  desktop: boolean
}) {
  const [open, setOpen] = useState(false)
  const { chip, focusAfterChange, onCloseAutoFocus } = useChipFocus(value)
  const groupId = useId()
  const visible = groups.filter((group) => group.options.length > 0)

  if (value) {
    return (
      <FilterChip
        ref={chip}
        label={label}
        value={value}
        onClear={() => {
          focusAfterChange()
          onClear()
        }}
      />
    )
  }

  const pick = (option: FilterOption) => {
    setOpen(false)
    onPick(option.value)
  }

  if (desktop) {
    return (
      <DropdownMenu open={open} onOpenChange={setOpen}>
        <DropdownMenuTrigger asChild>
          <FilterChip
            ref={chip}
            label={label}
            aria-label={`Filtrar por ${label.toLowerCase()}`}
          />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="w-auto max-w-80 min-w-64"
          onCloseAutoFocus={onCloseAutoFocus}
        >
          {visible.map((group, index) => (
            <DropdownMenuGroup
              key={group.title ?? index}
              // A screen reader announces "Gastos" or "Entradas" on entering the group.
              aria-labelledby={group.title ? `${groupId}-${index}` : undefined}
            >
              {group.title && (
                <DropdownMenuLabel id={`${groupId}-${index}`}>
                  {group.title}
                </DropdownMenuLabel>
              )}
              {group.options.map((option) => {
                const Icon = option.icon
                return (
                  <DropdownMenuItem key={option.value} onSelect={() => pick(option)}>
                    {Icon && (
                      <Icon
                        aria-hidden="true"
                        className={cn(option.pending && 'text-primary-text!')}
                      />
                    )}
                    <span className="min-w-0 flex-1 truncate">{option.label}</span>
                    {option.detail && (
                      <span className="shrink-0 pl-2 text-small font-medium text-muted-foreground">
                        {option.detail}
                      </span>
                    )}
                  </DropdownMenuItem>
                )
              })}
            </DropdownMenuGroup>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    )
  }

  return (
    <>
      <FilterChip
        ref={chip}
        label={label}
        aria-label={`Filtrar por ${label.toLowerCase()}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
      />
      <PickerSheet
        open={open}
        onOpenChange={setOpen}
        title={title}
        onCloseAutoFocus={onCloseAutoFocus}
      >
        {visible.map((group, index) => (
          <div key={group.title ?? index} role="group" aria-label={group.title}>
            {group.title && <PickerGroupTitle>{group.title}</PickerGroupTitle>}
            <ul className="flex flex-col">
              {group.options.map((option) => (
                <li key={option.value}>
                  <PickerRow
                    icon={option.icon}
                    pending={option.pending}
                    detail={option.detail}
                    onClick={() => pick(option)}
                  >
                    {option.label}
                  </PickerRow>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </PickerSheet>
    </>
  )
}
