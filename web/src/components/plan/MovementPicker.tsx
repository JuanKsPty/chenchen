/**
 * What the plan's sheets share to say which movement paid a payment, brought in a
 * paycheck or bought a planned purchase.
 *
 *   MovementLine    a movement in words: what it says, its day and its amount
 *   MovementChoice  the same as a 56px button: "Fue este: …" (`pick` says it for
 *                   a purchase: "Fue esta: …")
 *   MonthMovements  the rest of the month's spending (side 'out') or income ('in'),
 *                   all of it, closest to what was planned first, six before "Ver
 *                   más"
 *   PaidMovements   what paid it, under `title`, each with "No es este" (`reject`:
 *                   "No es esta" for a purchase), which the sheet sends
 *                   (usePlanLinks' notThis)
 *
 * MonthMovements is its own component so the month's list is fetched only once a
 * sheet is open (the sheets render, closed, with the page).
 */
import { useEffect, useId, useState } from 'react'

import type { EntryKind, PlanPaidMovement } from '@/api/types'
import { Amount } from '@/components/kit'
import { LoadError } from '@/components/spending/LoadError'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatCivilDayMonth, monthBounds } from '@/lib/format'
import { useEntryPages } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { monthWord } from './words'

/** The month's movements shown before "Ver más". */
const FIRST_SHOWN = 6

/**
 * How many movements each read of the month asks for: the most the movement list
 * gives at once (it refuses more), so a busier month takes a second read.
 */
const MONTH_PAGE = 200

/** A movement as a sheet lists it, from the plan or from the movement list. */
export interface Choice {
  id: string
  description: string
  booked_on: string
  amount: { cents: number }
  origin?: 'statement' | 'capture'
}

/** Which way the money went: a payment's spending, or a paycheck's income. */
export type Side = 'out' | 'in'

const KINDS: Record<Side, EntryKind[]> = {
  out: ['expense', 'fee', 'interest'],
  in: ['income'],
}

export function MovementLine({
  movement,
  className,
}: {
  movement: Choice
  className?: string
}) {
  return (
    <span className={cn('flex items-center gap-3', className)}>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-body font-medium">{movement.description}</span>
        <span className="text-small font-medium text-muted-foreground">
          {formatCivilDayMonth(movement.booked_on)}
          {movement.origin === 'capture' ? ' · Anotada' : ''}
        </span>
      </span>
      <Amount amount={movement.amount.cents} signed />
    </span>
  )
}

export function MovementChoice({
  movement,
  busy,
  onPick,
  pick = 'Fue este',
}: {
  movement: Choice
  busy: boolean
  onPick: (movement: Choice) => void
  pick?: string
}) {
  return (
    <button
      type="button"
      aria-disabled={busy || undefined}
      onClick={() => {
        if (!busy) onPick(movement)
      }}
      className="flex min-h-14 w-full cursor-pointer items-center rounded-2xl px-3 py-2 text-left text-foreground transition-colors duration-180 ease-out hover:bg-muted active:bg-muted"
    >
      <span className="sr-only">{`${pick}: `}</span>
      <MovementLine movement={movement} className="w-full" />
    </button>
  )
}

/**
 * The month's other movements on `side`, closest to `planned` (cents, positive)
 * first and then the latest, leaving out the ones `placed` already has a place for.
 *
 * The movement list comes newest first, so a page of it would leave out the month's
 * first days, which is when rent and most fixed payments go out. Every page is read
 * before any is shown: closest first only means something across the whole month. A
 * page that fails leaves the list unshown, since it would be missing days.
 */
export function MonthMovements({
  month,
  side,
  planned,
  placed,
  busy,
  onPick,
  pick,
}: {
  month: string
  side: Side
  planned: number
  placed: Set<string>
  busy: boolean
  onPick: (movement: Choice) => void
  pick?: string
}) {
  const { from, to } = monthBounds(month)
  const pages = useEntryPages({ from, to, kinds: KINDS[side], limit: MONTH_PAGE })
  const [all, setAll] = useState(false)
  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } = pages
  const reading = hasNextPage && !isFetchNextPageError

  useEffect(() => {
    if (reading && !isFetchingNextPage) void fetchNextPage()
  }, [reading, isFetchingNextPage, fetchNextPage])

  if (pages.isPending || reading) {
    return (
      <div className="flex flex-col gap-2" aria-hidden="true">
        {[0, 1, 2].map((row) => (
          <div key={row} className="flex min-h-14 items-center gap-3">
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3.5 w-16" />
            </div>
            <Skeleton className="h-4 w-16" />
          </div>
        ))}
      </div>
    )
  }
  if (!pages.data || isFetchNextPageError) return <LoadError />

  const size = (cents: number) => (side === 'out' ? -cents : cents)
  const list = pages.data.pages
    .flatMap((page) => page.entries)
    .filter((e) => size(e.amount.cents) > 0 && !placed.has(e.id))
    .sort(
      (a, b) =>
        Math.abs(size(a.amount.cents) - planned) -
          Math.abs(size(b.amount.cents) - planned) ||
        b.booked_on.localeCompare(a.booked_on),
    )
  const what = side === 'out' ? 'gastos' : 'ingresos'
  if (list.length === 0) {
    return (
      <p className="text-small font-medium text-pretty text-muted-foreground">
        {`No vemos otros ${what} en ${monthWord(month)}.`}
      </p>
    )
  }
  const shown = all ? list : list.slice(0, FIRST_SHOWN)
  return (
    <>
      <p className="text-small font-semibold text-muted-foreground">
        {`${side === 'out' ? 'Gastos' : 'Ingresos'} de ${monthWord(month)}`}
      </p>
      <ul className="-mx-3 flex flex-col">
        {shown.map((e) => (
          <li key={e.id}>
            <MovementChoice movement={e} busy={busy} onPick={onPick} pick={pick} />
          </li>
        ))}
      </ul>
      {!all && list.length > FIRST_SHOWN && (
        <Button variant="ghost" className="-ml-3 self-start" onClick={() => setAll(true)}>
          Ver más
        </Button>
      )}
    </>
  )
}

export function PaidMovements({
  title,
  movements,
  busy,
  onNotThis,
  reject = 'No es este',
}: {
  title: string
  movements: PlanPaidMovement[]
  busy: boolean
  onNotThis: (movement: PlanPaidMovement) => void
  reject?: string
}) {
  const titleId = useId()
  if (movements.length === 0) return null
  return (
    <section aria-labelledby={titleId} className="flex flex-col gap-2">
      <h3 id={titleId} className="text-body font-semibold">
        {title}
      </h3>
      <ul className="flex flex-col gap-2">
        {movements.map((m) => (
          <li key={m.id} className="flex items-center gap-2">
            <MovementLine movement={m} className="min-w-0 flex-1" />
            <Button
              variant="ghost"
              className="-mr-3"
              aria-label={`${reject}: ${m.description}`}
              aria-disabled={busy || undefined}
              onClick={() => onNotThis(m)}
            >
              {reject}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  )
}
