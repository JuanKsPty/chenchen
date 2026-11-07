/**
 * <PlanTrack amount cap size? className? />
 *   amount  what went out (cents or Amount); below zero (a refund) draws as nothing
 *   cap     what it was planned against: what is free, or a category's cap
 *   size    'md' (default) the 8px track · 'sm' 6px, a quincena's
 *
 * <PlanBar name amount cap onClick? to? className? />
 *   name     the category ("Alimentación")
 *   amount   what its free spending took this month
 *   cap      its cap
 *   onClick  the row is a <button> (opens the cap to edit it)
 *   to       the row is a link instead; neither: plain text (a past month)
 *
 * How a plan figure is drawn against what it was planned to be. Spec categoryBar's
 * track (surface2, 8px) filled in veranera up to `amount / cap`. Past the cap, the
 * bar fills the track: veranera up to `cap / amount`, a 2px cut in the card's colour
 * where the cap falls, then ink (foreground) for the rest, so the ink's share of the
 * bar is the share that went past it. Ink, like any outflow, and never orange or
 * red: spending more than planned is not an error (spec voice: nunca regañamos).
 * Not chart-2 either: in dark mode it is about as light as the veranera, and the
 * two parts have to differ in lightness, not only in hue (brief, accessibility).
 *
 * The track is decorative (aria-hidden); the caption under it says the same in
 * words: "Quedan $180.00 de $300.00", or "$42.10 más de lo previsto · tope
 * $300.00" in foreground 600 when it went past. Near the cap (80 %) nothing
 * changes: what is left already says so, and a second visual vocabulary (badges,
 * colours) would only add noise.
 *
 * CategoryBar stays as it is: it scales a set to its largest, which is never past.
 * The widths are ratios of integer cents, for display only.
 */
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import type { Amount as AmountValue } from '@/api/types'
import { cn } from '@/lib/utils'
import { Amount } from './Amount'

const cents = (value: AmountValue | number) =>
  typeof value === 'number' ? value : value.cents

export function PlanTrack({
  amount,
  cap,
  size = 'md',
  className,
}: {
  amount: AmountValue | number
  cap: AmountValue | number
  size?: 'md' | 'sm'
  className?: string
}) {
  const spent = Math.max(0, cents(amount))
  const limit = Math.max(0, cents(cap))
  const over = spent > limit
  const height = size === 'sm' ? 'h-1.5' : 'h-2'

  return (
    <span
      aria-hidden="true"
      className={cn('flex w-full overflow-hidden rounded-full bg-muted', height, className)}
    >
      {over ? (
        <>
          {limit > 0 && (
            <span
              className="block h-full min-w-2 shrink-0 bg-primary transition-[width] duration-280 ease-out motion-reduce:transition-none"
              style={{ width: `${(limit / spent) * 100}%` }}
            />
          )}
          {limit > 0 && <span className="block h-full w-0.5 shrink-0 bg-card" />}
          <span className="block h-full min-w-2 flex-1 bg-foreground" />
        </>
      ) : (
        spent > 0 && (
          <span
            className="block h-full min-w-2 rounded-full bg-primary transition-[width] duration-280 ease-out motion-reduce:transition-none"
            style={{ width: `${(spent / limit) * 100}%` }}
          />
        )
      )}
    </span>
  )
}

/**
 * "Quedan $180.00 de $300.00", or, past the cap, "$42.10 más de lo previsto" in
 * foreground 600 and " · tope $300.00" muted. `of` names the second figure: "tope"
 * for a cap, "libres" for what is free.
 */
export function PlanCaption({
  amount,
  cap,
  of = 'tope',
  className,
}: {
  amount: AmountValue | number
  cap: AmountValue | number
  of?: 'tope' | 'libres'
  className?: string
}) {
  const spent = Math.max(0, cents(amount))
  const limit = cents(cap)
  const over = spent > limit
  let text: ReactNode
  if (over) {
    text = (
      <>
        <span className="font-semibold text-foreground">
          <Amount amount={spent - limit} size="inline" /> más de lo previsto
        </span>
        {of === 'tope' ? (
          <>
            {' '}
            · tope <Amount amount={limit} size="inline" />
          </>
        ) : (
          <>
            {' '}
            · <Amount amount={limit} size="inline" /> libres
          </>
        )}
      </>
    )
  } else {
    text = (
      <>
        Quedan <Amount amount={limit - spent} size="inline" /> de{' '}
        <Amount amount={limit} size="inline" />
      </>
    )
  }
  return (
    <span
      className={cn(
        'block text-small font-medium text-pretty text-muted-foreground',
        className,
      )}
    >
      {text}
    </span>
  )
}

export function PlanBar({
  name,
  amount,
  cap,
  onClick,
  to,
  className,
}: {
  name: string
  amount: AmountValue
  cap: AmountValue | number
  onClick?: () => void
  to?: string
  className?: string
}) {
  const content = (
    <>
      <span className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-3">
        <span className="truncate text-body font-medium group-hover:underline group-hover:underline-offset-4">
          {name}
        </span>
        <Amount amount={amount} />
      </span>
      <PlanTrack amount={amount} cap={cap} />
      <PlanCaption amount={amount} cap={cap} />
    </>
  )

  const layout = cn(
    'group flex min-h-16 w-full flex-col justify-center gap-2 py-1 text-left text-foreground',
    className,
  )
  if (onClick) {
    return (
      <button type="button" onClick={onClick} className={cn(layout, 'cursor-pointer')}>
        {content}
      </button>
    )
  }
  if (to) {
    return (
      <Link to={to} className={layout}>
        {content}
      </Link>
    )
  }
  return <div className={layout}>{content}</div>
}
