/**
 * <MonthPicker month size? className? />
 *
 *   month  the month on screen, YYYY-MM
 *   size   'bar'  (default) 40px chevrons and a small label: beside the desktop h1
 *          'wide' 44px chevrons, body label, stretched: the mobile page's first row
 *
 * "‹ Octubre 2026 ›" on a surface2 pill. The chevrons are links to ?mes (the current
 * month drops the parameter), so the month lives in the URL and Back works. There is
 * nothing after the current month: its "›" is disabled. Stepping onto the current
 * month with "›" would leave focus on that disabled button (lost to the page), so
 * focus moves to "‹" instead. The label is a polite live region, so a screen reader
 * hears the new month after each step.
 */
import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { currentMonth, monthTitle, shiftMonth } from '@/lib/format'
import { spendingHref } from '@/lib/nav'
import { cn } from '@/lib/utils'

export function MonthPicker({
  month,
  size = 'bar',
  className,
}: {
  month: string
  size?: 'bar' | 'wide'
  className?: string
}) {
  const now = currentMonth()
  const previous = shiftMonth(month, -1)
  const next = shiftMonth(month, 1)
  const hrefOf = (ym: string) => spendingHref(ym === now ? undefined : ym)
  const buttonSize = size === 'wide' ? 'icon' : 'icon-bar'
  const buttonClass = 'hover:bg-line'
  const previousRef = useRef<HTMLAnchorElement>(null)
  const refocusPrevious = useRef(false)

  useEffect(() => {
    if (!refocusPrevious.current) return
    refocusPrevious.current = false
    previousRef.current?.focus()
  }, [month])

  return (
    <div
      role="group"
      aria-label="Mes"
      className={cn(
        'flex items-center justify-between gap-1 rounded-full bg-muted',
        className,
      )}
    >
      <Button asChild variant="plain" size={buttonSize} className={buttonClass}>
        <Link
          ref={previousRef}
          to={hrefOf(previous)}
          aria-label={`Mes anterior, ${monthTitle(previous)}`}
        >
          <ChevronLeftIcon aria-hidden="true" />
        </Link>
      </Button>
      <span
        aria-live="polite"
        className={cn(
          'min-w-0 truncate px-1 text-center font-semibold',
          size === 'wide' ? 'text-body' : 'min-w-28 text-small',
        )}
      >
        {monthTitle(month)}
      </span>
      {next <= now ? (
        <Button asChild variant="plain" size={buttonSize} className={buttonClass}>
          <Link
            to={hrefOf(next)}
            aria-label={`Mes siguiente, ${monthTitle(next)}`}
            onClick={(event) => {
              // Only when focus is on this link, which the step is about to remove.
              refocusPrevious.current =
                next === now && document.activeElement === event.currentTarget
            }}
          >
            <ChevronRightIcon aria-hidden="true" />
          </Link>
        </Button>
      ) : (
        <Button
          type="button"
          variant="plain"
          size={buttonSize}
          disabled
          aria-label="Mes siguiente"
        >
          <ChevronRightIcon aria-hidden="true" />
        </Button>
      )}
    </div>
  )
}
