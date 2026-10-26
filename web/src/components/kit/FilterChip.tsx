/**
 * <FilterChip label value? onClear? className? ...buttonProps />
 *
 *   label    the filter's name: "Cuenta", "Categoría", "Fechas", "Tipo"
 *   value    the chosen value as words ("Corriente ···0932", "Alimentación",
 *            "01–24 oct"), or null/undefined when the filter is off
 *   onClear  removes the filter
 *
 * Off: surface2 chip with the label and a 16px chevron-down. Every other prop
 * (onClick, aria-*) goes to the <button>, so it can be a Radix trigger:
 *   <PopoverTrigger asChild><FilterChip label="Cuenta" /></PopoverTrigger>
 * On: primary-soft chip with the value and a 16px x; the whole chip removes the
 * filter (aria-label "Quitar filtro Cuenta: Corriente ···0932") and any onClick from a
 * trigger is ignored, so render it outside the trigger or let it be.
 *
 * Spec input/search: 44px tall below lg (touch), 36 from lg, padding 0 12 0 16, pill,
 * Figtree 14/20 600. In a row with gap 8; on mobile the row scrolls sideways.
 */
import { ChevronDownIcon, XIcon } from 'lucide-react'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

export function FilterChip({
  label,
  value,
  onClear,
  className,
  onClick,
  ...props
}: Omit<ComponentProps<'button'>, 'value'> & {
  label: string
  value?: string | null
  onClear?: () => void
}) {
  const active = value != null && value !== ''

  return (
    <button
      type="button"
      {...props}
      onClick={active ? () => onClear?.() : onClick}
      aria-label={active ? `Quitar filtro ${label}: ${value}` : props['aria-label']}
      className={cn(
        'inline-flex h-11 shrink-0 items-center gap-1.5 rounded-full pr-3 pl-4 text-small font-semibold whitespace-nowrap transition-colors duration-180 ease-out lg:h-9 [&_svg]:size-4 [&_svg]:shrink-0',
        active
          ? 'bg-primary-soft text-primary-text hover:bg-primary-soft/70'
          : 'bg-muted text-foreground hover:bg-line aria-expanded:bg-line',
        className,
      )}
    >
      {active ? value : label}
      {active ? <XIcon aria-hidden="true" /> : <ChevronDownIcon aria-hidden="true" />}
    </button>
  )
}
