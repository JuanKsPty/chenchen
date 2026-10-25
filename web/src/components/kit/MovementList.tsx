/**
 * <MovementList entries accounts? categories? showAccount? groupByDay? compact?
 *               loading? empty? footer? onSelect? selectable? dayHeadingClassName?
 *               headingLevel? className? />
 *
 *   entries              newest first, as the API returns them
 *   accounts             to label each row's account ("Tarjeta ···1111")
 *   categories           the category tree (useCategories().data?.categories)
 *   showAccount          true (default). Off on an account's own page. In a list 44rem
 *                        or wider the account is a 160px column (with "Categorizar"
 *                        before it on uncategorized rows); narrower, it joins the
 *                        second line.
 *   groupByDay           true (default): under day headings ("Sábado 24 oct", small
 *                        600 muted). false: one flat list with the day in each row's
 *                        second line (Inicio's Recientes).
 *   compact              false (default). true keeps every row narrow at any width:
 *                        the account in the second line, no account or Categorizar
 *                        column (Inicio's Recientes, inside a narrow card column).
 *   loading              with no entries yet, draws 5 skeleton rows
 *   empty                what to show when there are no entries (e.g. an <EmptyState>)
 *   footer               under the rows: "Ver más", a link to Movimientos…
 *   onSelect             called with the tapped entry instead of the built-in sheet
 *   selectable           false makes rows static (no button, no sheet). Default true.
 *   dayHeadingClassName  extra classes for the day headings, e.g. to make them sticky
 *                        under a search bar: "sticky top-14 z-10 bg-background"
 *   headingLevel         level of the day headings: 3 (default) or 2
 *
 * Tapping a row opens the categorize sheet for that movement (unless `onSelect` is
 * given). The list sets an @container, so the wide layout follows its own width,
 * not the viewport's. Put it in a SectionCard layout="list", or straight on the page
 * on mobile Movimientos.
 */
import { useMemo, useState, type ReactNode } from 'react'

import type { Account, CategoryNode, Entry } from '@/api/types'
import { CategorizeSheet } from '@/components/movements/CategorizeSheet'
import { Skeleton } from '@/components/ui/skeleton'
import { categoryNames } from '@/lib/categories'
import { dayHeading } from '@/lib/format'
import { cn } from '@/lib/utils'
import { MovementRow } from './MovementRow'

interface DayGroup {
  day: string
  entries: Entry[]
}

/** Consecutive entries on the same day, keeping the API's newest-first order. */
function groupByBookedDay(entries: Entry[]): DayGroup[] {
  const groups: DayGroup[] = []
  for (const entry of entries) {
    const last = groups[groups.length - 1]
    if (last && last.day === entry.booked_on) last.entries.push(entry)
    else groups.push({ day: entry.booked_on, entries: [entry] })
  }
  return groups
}

export function MovementList({
  entries,
  accounts = [],
  categories,
  showAccount = true,
  groupByDay = true,
  compact = false,
  loading = false,
  empty,
  footer,
  onSelect,
  selectable = true,
  dayHeadingClassName,
  headingLevel = 3,
  className,
}: {
  entries: Entry[]
  accounts?: Account[]
  categories?: CategoryNode[]
  showAccount?: boolean
  groupByDay?: boolean
  compact?: boolean
  loading?: boolean
  empty?: ReactNode
  footer?: ReactNode
  onSelect?: (entry: Entry) => void
  selectable?: boolean
  dayHeadingClassName?: string
  headingLevel?: 2 | 3
  className?: string
}) {
  const [selected, setSelected] = useState<Entry | null>(null)
  const byId = useMemo(() => new Map(accounts.map((a) => [a.id, a])), [accounts])
  const names = useMemo(() => categoryNames(categories), [categories])
  const Heading = headingLevel === 2 ? 'h2' : 'h3'

  const select = selectable ? (onSelect ?? setSelected) : undefined

  const row = (entry: Entry) => (
    <li key={entry.id}>
      <MovementRow
        entry={entry}
        account={byId.get(entry.account_id)}
        categoryName={entry.category_id ? names.get(entry.category_id) : undefined}
        showAccount={showAccount}
        showDate={!groupByDay}
        compact={compact}
        onSelect={select}
      />
    </li>
  )

  let body: ReactNode
  if (entries.length === 0) {
    body = loading ? <MovementSkeleton /> : empty
  } else if (groupByDay) {
    body = groupByBookedDay(entries).map((group) => (
      <div key={group.day} className="flex flex-col">
        <Heading
          className={cn(
            'flex min-h-9 items-end px-4 pt-3 pb-1 text-small font-semibold text-muted-foreground',
            dayHeadingClassName,
          )}
        >
          {dayHeading(group.day)}
        </Heading>
        <ul className="flex flex-col">{group.entries.map(row)}</ul>
      </div>
    ))
  } else {
    body = <ul className="flex flex-col">{entries.map(row)}</ul>
  }

  return (
    <div className={cn('@container flex min-w-0 flex-col', className)}>
      {body}
      {footer}
      {selectable && !onSelect && (
        <CategorizeSheet
          entry={selected}
          open={selected !== null}
          onOpenChange={(open) => {
            if (!open) setSelected(null)
          }}
        />
      )}
    </div>
  )
}

function MovementSkeleton() {
  return (
    <ul aria-hidden="true" className="flex flex-col">
      {Array.from({ length: 5 }, (_, index) => (
        <li
          key={index}
          className="grid min-h-16 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 md:min-h-14 md:py-2"
        >
          <Skeleton className="size-10 rounded-full" />
          <span className="flex flex-col gap-2">
            <Skeleton className="h-4 w-3/5 rounded-full" />
            <Skeleton className="h-3 w-2/5 rounded-full" />
          </span>
          <Skeleton className="h-4 w-16 rounded-full" />
        </li>
      ))}
    </ul>
  )
}
