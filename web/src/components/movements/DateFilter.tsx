/**
 * <DateFilter from? to? onChange onClear desktop />
 *
 *   from, to  the range in the URL (YYYY-MM-DD, inclusive), either may be missing
 *   onChange  called with the new { from?, to? }
 *   onClear   removes the filter
 *   desktop   from lg the choices open in a popover under the chip; below it, in a
 *             bottom sheet
 *
 * The "Fechas" chip. Off, it offers four ranges that cover most questions (Este mes,
 * Mes pasado, Últimos 90 días, Este año), each with the dates it means, and "Otras
 * fechas": Desde and Hasta typed or picked, then "Aplicar". On, the chip reads the
 * range ("01–24 oct") and clears it.
 */
import { useState } from 'react'

import { DateField } from '@/components/DateField'
import { FilterChip } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  currentMonth,
  formatRange,
  monthBounds,
  shiftMonth,
  todayCivil,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import { useChipFocus } from './OptionFilter'
import { PickerRow, PickerSheet } from './PickerSheet'

interface Range {
  from?: string
  to?: string
}

/** A civil date `days` away from `ymd`, built from its parts so no zone shifts it. */
function addDays(ymd: string, days: number): string {
  const [year = 0, month = 1, day = 1] = ymd.split('-').map(Number)
  const date = new Date(year, month - 1, day + days)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function presets(): Array<{ label: string; range: Required<Range> }> {
  const today = todayCivil()
  const month = currentMonth()
  return [
    { label: 'Este mes', range: { from: monthBounds(month).from, to: today } },
    { label: 'Mes pasado', range: monthBounds(shiftMonth(month, -1)) },
    { label: 'Últimos 90 días', range: { from: addDays(today, -89), to: today } },
    { label: 'Este año', range: { from: `${today.slice(0, 4)}-01-01`, to: today } },
  ]
}

export function DateFilter({
  from,
  to,
  onChange,
  onClear,
  desktop,
}: Range & {
  onChange: (range: Range) => void
  onClear: () => void
  desktop: boolean
}) {
  const [open, setOpen] = useState(false)
  const value = from || to ? formatRange(from, to) : undefined
  const { chip, focusAfterChange, onCloseAutoFocus } = useChipFocus(value)

  if (value) {
    return (
      <FilterChip
        ref={chip}
        label="Fechas"
        value={value}
        onClear={() => {
          focusAfterChange()
          onClear()
        }}
      />
    )
  }

  const choose = (range: Range) => {
    setOpen(false)
    onChange(range)
  }

  const panel = <DatePanel desktop={desktop} onChoose={choose} />

  if (desktop) {
    return (
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <FilterChip ref={chip} label="Fechas" aria-label="Filtrar por fechas" />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="w-80 gap-0 p-1.5"
          onCloseAutoFocus={onCloseAutoFocus}
        >
          {panel}
        </PopoverContent>
      </Popover>
    )
  }

  return (
    <>
      <FilterChip
        ref={chip}
        label="Fechas"
        aria-label="Filtrar por fechas"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
      />
      <PickerSheet
        open={open}
        onOpenChange={setOpen}
        title="Filtrar por fechas"
        onCloseAutoFocus={onCloseAutoFocus}
      >
        {panel}
      </PickerSheet>
    </>
  )
}

function DatePanel({
  desktop,
  onChoose,
}: {
  desktop: boolean
  onChoose: (range: Range) => void
}) {
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  // A field showing a half-typed or impossible date: its value is still the last
  // real date, which Aplicar must not send in place of what is on screen.
  const [fromIncomplete, setFromIncomplete] = useState(false)
  const [toIncomplete, setToIncomplete] = useState(false)
  const inverted = Boolean(from && to && from > to)
  const ready = Boolean(from || to) && !inverted && !fromIncomplete && !toIncomplete

  return (
    <div className="flex flex-col">
      <ul className="flex flex-col" aria-label="Rangos rápidos">
        {presets().map((preset) => (
          <li key={preset.label}>
            <PickerRow
              className={cn(desktop && 'min-h-11')}
              detail={formatRange(preset.range.from, preset.range.to)}
              onClick={() => onChoose(preset.range)}
            >
              {preset.label}
            </PickerRow>
          </li>
        ))}
      </ul>

      <form
        className="flex flex-col gap-3 px-3 pt-2 pb-2"
        aria-label="Otras fechas"
        onSubmit={(event) => {
          event.preventDefault()
          if (ready) onChoose({ from: from || undefined, to: to || undefined })
        }}
      >
        <h3 className="pt-2 text-small font-semibold text-muted-foreground">
          Otras fechas
        </h3>
        <DateField
          label="Desde"
          value={from}
          onChange={setFrom}
          onIncompleteChange={setFromIncomplete}
          max={to || undefined}
        />
        <DateField
          label="Hasta"
          value={to}
          onChange={setTo}
          onIncompleteChange={setToIncomplete}
          min={from || undefined}
        />
        <Button
          type="submit"
          size={desktop ? 'default' : 'lg'}
          className="mt-1 w-full"
          disabled={!ready}
        >
          Aplicar
        </Button>
      </form>
    </div>
  )
}
