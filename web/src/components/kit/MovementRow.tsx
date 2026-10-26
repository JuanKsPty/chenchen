/**
 * <MovementRow entry account? categoryName? showAccount? showDate? compact? onSelect?
 *              className? />
 *
 *   entry         the Entry
 *   account       its Account, for the account label ("Tarjeta ···1111")
 *   categoryName  the entry's category name (categoryNames(tree).get(entry.category_id))
 *   showAccount   true (default): show which account. In a wide list (the list's
 *                 container is 44rem or more) it is its own 160px column before the
 *                 amount; otherwise it joins the second line. Off on an account's page.
 *   showDate      add the day ("24 oct") to the second line, for lists that are not
 *                 grouped by day (Inicio's Recientes).
 *   compact       always the narrow row: the account stays in the second line and
 *                 there is no "Categorizar" column, however wide the list.
 *   onSelect      makes the row a <button> (Categorizar); without it the row is static.
 *
 * Spec movementRow: grid [40 | 1fr | auto], gap 12, padding 10 16 (8 16 from md), at
 * least 64px tall on mobile and 56 from md; hover and press surface2 as a pill.
 *   - Category tile (40, icon 20 muted). "Sin categoría" is primary-soft with
 *     circle-help in primary-text: a to-do, not an error.
 *   - Line 1: the bank's description as printed (capitals, no tracking), body 500,
 *     one line with an ellipsis.
 *   - Line 2, small 500 muted: category · day (with showDate) · account (unless it
 *     has its own column) · the owner's note: "Alimentación · 24 oct · Tarjeta ···1111".
 *   - Right: the signed amount, body 600 tabular. In: "+$3,800.00" in positive.
 *     Out: "−$84.37" in the text colour.
 *   - Wide lists also show "Categorizar" in primary-text on uncategorized rows.
 *
 * Wide-layout columns respond to the nearest @container (MovementList sets one). The
 * threshold is 44rem because of the "Categorizar" column: tile 40 + Categorizar 110 +
 * account 160 + amount 112 + four 12px gaps + 32 of padding leave the description
 * about 12.7rem at 44rem (measured), and never less than 12rem above it. At the
 * old 36rem it got about 70px.
 */
import type { Account, Entry } from '@/api/types'
import { accountWithNumber } from '@/lib/account'
import { movementCategory } from '@/lib/categories'
import { entryKindLabel, formatCivilDayMonth } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Amount } from './Amount'
import { Tile } from './Tile'

export function MovementRow({
  entry,
  account,
  categoryName,
  showAccount = true,
  showDate = false,
  compact = false,
  onSelect,
  className,
}: {
  entry: Entry
  account?: Account
  categoryName?: string
  showAccount?: boolean
  showDate?: boolean
  compact?: boolean
  onSelect?: (entry: Entry) => void
  className?: string
}) {
  const category = movementCategory(entry, categoryName)
  const accountText = showAccount && account ? accountWithNumber(account) : undefined
  const accountColumn = Boolean(accountText) && !compact

  const meta = (
    <>
      {category.label}
      {showDate && ` · ${formatCivilDayMonth(entry.booked_on)}`}
      {accountText && (
        <span className={cn(accountColumn && '@min-[44rem]:hidden')}> · {accountText}</span>
      )}
      {entry.note && ` · ${entry.note}`}
    </>
  )

  const content = (
    <>
      <Tile icon={category.icon} pending={category.pending} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-body font-medium">
          {entry.description || entryKindLabel(entry.kind)}
        </span>
        <span className="truncate text-small font-medium text-muted-foreground">
          {meta}
        </span>
      </span>
      {accountColumn && (
        <>
          <span className="hidden @min-[44rem]:block">
            {category.pending && onSelect && (
              <span className="inline-flex h-9 items-center px-3 text-body font-semibold whitespace-nowrap text-primary-text">
                Categorizar
              </span>
            )}
          </span>
          <span className="hidden truncate text-small font-medium text-muted-foreground @min-[44rem]:block">
            {accountText}
          </span>
        </>
      )}
      <Amount amount={entry.amount} signed className="text-right" />
    </>
  )

  const layout = cn(
    'grid w-full grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 rounded-full px-4 py-2.5 text-left text-foreground min-h-16 md:min-h-14 md:py-2',
    accountColumn && '@min-[44rem]:grid-cols-[2.5rem_minmax(0,1fr)_auto_10rem_7rem]',
    className,
  )

  if (!onSelect) return <div className={layout}>{content}</div>

  return (
    <button
      type="button"
      aria-haspopup="dialog"
      onClick={() => onSelect(entry)}
      className={cn(
        layout,
        'transition-colors duration-180 ease-out hover:bg-muted active:bg-muted',
        // The focus ring inside the pill: on a phone the row runs edge to edge and
        // the sticky day headings sit right above and below it, so a ring outside
        // would be cut off on every side.
        '-outline-offset-2',
      )}
    >
      {content}
    </button>
  )
}
