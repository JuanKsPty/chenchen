/**
 * <OutflowCard period />
 *
 * The month's headline card (C-Gastos-Desktop): "Salidas de octubre" over the total
 * in display, a short line under it ("Al 24 oct. Septiembre cerró en $3,288.09."),
 * and beside it, or under it on a narrow screen, "Entradas y salidas" for the four
 * months ending with this one. Transfers between the owner's accounts and trades
 * are not in either figure (the flow report leaves them out).
 */
import { useId } from 'react'

import { Amount } from '@/components/kit'
import { Skeleton } from '@/components/ui/skeleton'
import { formatCivilDayMonth, formatRange, monthBounds, shiftMonth } from '@/lib/format'
import { useFlow } from '@/lib/queries'
import { MonthChart, type MonthFlow } from './MonthChart'
import { monthInSentence, monthName, type SpendingPeriod } from './period'
import { LoadError } from './LoadError'

const MONTHS_SHOWN = 4

export function OutflowCard({ period }: { period: SpendingPeriod }) {
  const headingId = useId()
  const first = shiftMonth(period.month, -(MONTHS_SHOWN - 1))
  const flow = useFlow({
    from: monthBounds(first).from,
    to: period.to,
    granularity: 'month',
  })

  const byMonth = new Map(
    (flow.data?.points ?? []).map((p) => [p.day.slice(0, 7), p] as const),
  )
  const months: MonthFlow[] = Array.from({ length: MONTHS_SHOWN }, (_, i) => {
    const month = shiftMonth(first, i)
    const point = byMonth.get(month)
    return { month, in: point?.in.cents ?? 0, out: point?.out.cents ?? 0 }
  })
  const thisMonth = months[MONTHS_SHOWN - 1]
  const lastMonth = months[MONTHS_SHOWN - 2]

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-wrap gap-6 rounded-2xl bg-card p-4 text-card-foreground shadow-card md:gap-8 md:p-6"
    >
      {/* The footnote stays with the figure it explains, not pushed down to the
          chart's baseline: beside the chart that left a band of nothing between
          them. */}
      <div className="flex min-w-0 flex-[1_1_15rem] flex-col gap-3">
        <div className="flex flex-col gap-2">
          <h2 id={headingId} className="text-body font-semibold">
            Salidas de {monthInSentence(period.month)}
          </h2>
          {flow.isPending ? (
            <Skeleton className="h-13 w-56" />
          ) : (
            // A failed load has no figure: "$0.00" would say nothing went out.
            !flow.isError && (
              <p>
                <Amount amount={thisMonth?.out ?? 0} size="display" />
              </p>
            )
          )}
        </div>
        {flow.isError ? (
          <LoadError />
        ) : (
          !flow.isPending && (
            <p className="text-small font-medium text-pretty text-muted-foreground">
              {period.current
                ? `Al ${formatCivilDayMonth(period.to)}.`
                : `${formatRange(period.from, period.to)}.`}
              {lastMonth && lastMonth.out > 0 && (
                <>
                  {' '}
                  {monthName(lastMonth.month)} cerró en{' '}
                  <Amount amount={lastMonth.out} size="inline" />.
                </>
              )}
            </p>
          )
        )}
      </div>

      {flow.isPending ? (
        <Skeleton className="h-60 min-w-0 flex-[1.4_1_18.75rem]" />
      ) : (
        !flow.isError && (
          <MonthChart
            months={months}
            selected={period.month}
            partial={period.current}
            className="flex-[1.4_1_18.75rem]"
          />
        )
      )}
    </section>
  )
}
