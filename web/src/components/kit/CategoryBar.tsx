/**
 * <CategoryBar name amount max to? className? />
 *   name    the category ("Vivienda")
 *   amount  its total for the period
 *   max     the largest total in the set (cents or Amount): that one fills the track
 *   to      where it links, normally movementsHref({ category, from, to })
 *
 * <CategoryBar variant="uncategorized" amount count to className? />
 *   "Sin categoría", no bar: "$219.53 · 7 movimientos" in small muted and a ghost
 *   "Categorizar" linking to `to` (movementsHref({ uncategorized: true, from, to })).
 *   It goes last: it is a to-do, not a category.
 *
 * Spec categoryBar: a link at least 52px tall, column with gap 8, padding 4 0.
 * Line 1: name (body 500) and amount (body 600 tabular). Line 2: an 8px track in
 * surface2 filled in veranera by amount / max. Every category the same colour.
 * The uncategorized row follows C-Gastos-Desktop: a surface2 pill with the pending
 * tile. When the row itself is narrower than 24rem (a phone), "$219.53 · 7
 * movimientos" keeps its one line and "Categorizar" moves under it, so the row
 * becomes a 16px-radius card instead of a pill.
 */
import { Link } from 'react-router-dom'

import type { Amount as AmountValue } from '@/api/types'
import { Button } from '@/components/ui/button'
import { UNCATEGORIZED_ICON, UNCATEGORIZED_LABEL } from '@/lib/categories'
import { cn } from '@/lib/utils'
import { Amount } from './Amount'
import { Tile } from './Tile'

type CategoryBarProps =
  | {
      variant?: 'category'
      name: string
      amount: AmountValue
      max: AmountValue | number
      to?: string
      className?: string
    }
  | {
      variant: 'uncategorized'
      amount: AmountValue
      count: number
      to: string
      className?: string
    }

export function CategoryBar(props: CategoryBarProps) {
  if (props.variant === 'uncategorized') return <UncategorizedBar {...props} />

  const { name, amount, max, to, className } = props
  const maxCents = typeof max === 'number' ? max : max.cents
  const share = maxCents > 0 ? Math.min(1, Math.max(0, amount.cents / maxCents)) : 0

  const content = (
    <>
      <span className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-3">
        <span className="truncate text-body font-medium group-hover:underline group-hover:underline-offset-4">
          {name}
        </span>
        <Amount amount={amount} />
      </span>
      <span aria-hidden="true" className="block h-2 overflow-hidden rounded-full bg-muted">
        <span
          className="block h-2 min-w-2 rounded-full bg-primary"
          style={{ width: `${share * 100}%` }}
        />
      </span>
    </>
  )

  const layout = cn('group flex min-h-13 flex-col justify-center gap-2 py-1', className)
  return to ? (
    <Link to={to} className={cn(layout, 'text-foreground')}>
      {content}
    </Link>
  ) : (
    <div className={layout}>{content}</div>
  )
}

function UncategorizedBar({
  amount,
  count,
  to,
  className,
}: {
  amount: AmountValue
  count: number
  to: string
  className?: string
}) {
  return (
    // The row's own width decides, not the viewport's: Gastos puts it in a card
    // column whose width depends on the assistant panel too.
    <div className={cn('@container', className)}>
      <div
        className={cn(
          'grid grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-x-3 rounded-2xl bg-muted p-2 pb-1',
          '@sm:grid-cols-[2.5rem_minmax(0,1fr)_auto] @sm:rounded-full @sm:pb-2',
        )}
      >
        <Tile icon={UNCATEGORIZED_ICON} pending />
        <span className="flex min-w-0 flex-col">
          <span className="truncate text-body font-medium">{UNCATEGORIZED_LABEL}</span>
          <span className="truncate text-small font-medium text-muted-foreground">
            <Amount amount={amount} size="inline" /> · {count}{' '}
            {count === 1 ? 'movimiento' : 'movimientos'}
          </span>
        </span>
        {/* Under the text on a phone, its label lined up with "Sin categoría". */}
        <Button
          asChild
          variant="ghost"
          className="col-start-2 -ml-5 justify-self-start hover:bg-line @sm:col-start-auto @sm:ml-0"
        >
          <Link to={to}>Categorizar</Link>
        </Button>
      </div>
    </div>
  )
}
