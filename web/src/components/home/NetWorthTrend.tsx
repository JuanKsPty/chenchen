/**
 * <NetWorthTrend />
 *
 * The net worth month by month, inside the veranera block of NetWorthCard: how much it
 * moved since the first month there is ("+$1,240.00 desde junio 2026") and the line
 * (kit/TrendChart, inverse). It reads the last 12 months' ends and today
 * (useNetWorthHistory); the history starts where the owner's statements do, so a new
 * owner sees a shorter line, and nothing at all with a single point.
 *
 * A month some account could not be read is dashed, and the change says so: it
 * compares what there was to read, not all of it.
 */
import { TrendChart } from '@/components/kit/TrendChart'
import { MINUS, money, monthTitle } from '@/lib/format'
import { useNetWorthHistory } from '@/lib/queries'

export function NetWorthTrend() {
  const history = useNetWorthHistory({ months: 12 })
  const points = history.data?.points ?? []
  const first = points[0]
  const last = points.at(-1)
  if (points.length < 2 || !first || !last) return null

  const change = last.total.cents - first.total.cents
  const partial = points.some((p) => p.partial)
  const sign = change > 0 ? '+' : change < 0 ? MINUS : ''

  return (
    <div className="flex flex-col gap-3">
      <p className="text-small text-primary-foreground/80">
        <span className="font-semibold text-primary-foreground tabular-nums">
          {sign}
          {money(Math.abs(change))}
        </span>{' '}
        desde {monthTitle(first.day).toLowerCase()}
        {partial && ' · algunos meses están incompletos'}
      </p>
      <TrendChart
        tone="inverse"
        label="Patrimonio neto, mes a mes"
        points={points.map((p) => ({
          day: p.day,
          cents: p.total.cents,
          partial: p.partial,
        }))}
      />
    </div>
  )
}
