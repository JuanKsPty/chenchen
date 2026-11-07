/**
 * <PlanRow icon title subtitle amount onClick? footer? />
 *
 * One item of the plan in a card's list (a fixed payment, a saving, a planned
 * purchase, a card's due date): its tile, its name in 600, a line in small muted
 * under it, and its amount at the right. With `onClick` the row is a <button> whose
 * accessible name is all it says; without it, plain text (a month that can no longer
 * be changed, or a card, which is only there to be read). 64px tall on a phone and 56
 * from md, like a rule or a movement; the status line wraps rather than losing its
 * date.
 *
 * `footer` goes under the text and the amount, across both, inside the row: a track
 * and its caption ("Apartado en el plan: $440.00 de $1,100.00"), which the row's
 * name then says too, since the track itself is only drawn.
 *
 * Its list goes in a <ul className={PLAN_LIST}>, which pulls the rows out to the
 * card's edge so their content lines up with the card's heading.
 */
import type { LucideIcon } from 'lucide-react'
import type { MouseEvent, ReactNode } from 'react'

import { Tile } from '@/components/kit'
import { cn } from '@/lib/utils'

export const PLAN_LIST = '-mx-3 flex flex-col gap-1 md:-mx-4'

export function PlanRow({
  icon,
  title,
  subtitle,
  amount,
  onClick,
  footer,
}: {
  icon: LucideIcon
  title: string
  subtitle: ReactNode
  amount?: ReactNode
  onClick?: (event: MouseEvent<HTMLButtonElement>) => void
  footer?: ReactNode
}) {
  const content = (
    <>
      <Tile icon={icon} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-body font-semibold">{title}</span>
        <span className="text-small font-medium text-pretty text-muted-foreground">
          {subtitle}
        </span>
      </span>
      {amount ?? <span />}
      {footer && (
        <span className="col-span-2 col-start-2 -mt-1 flex min-w-0 flex-col gap-1.5 pb-1">
          {footer}
        </span>
      )}
    </>
  )
  const layout =
    'grid min-h-16 w-full grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 rounded-[1.75rem] px-3 py-2.5 text-left text-foreground md:min-h-14 md:px-4 md:py-2'
  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        className={cn(
          layout,
          'cursor-pointer transition-colors duration-180 ease-out hover:bg-muted active:bg-muted',
        )}
      >
        {content}
      </button>
    )
  }
  return <div className={layout}>{content}</div>
}
