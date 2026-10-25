/**
 * <MonthFlow className? />
 *
 * "Este mes": money in and out of the owner's accounts this month (spec monthChart,
 * the Inicio part), transfers between their own accounts left out. Two horizontal
 * 12px bars on a surface2 track, the bigger figure filling its track: Entradas in
 * positive, Salidas in chart-out (ink, never red). Each figure in body 600.
 *
 * Mobile (C-Inicio-Mobile): two columns, the label small and muted over the figure.
 * From lg (C-Inicio-Desktop): stacked rows, label and figure on one line.
 */
import { Amount, SectionCard } from '@/components/kit'
import { Skeleton } from '@/components/ui/skeleton'
import { currentMonth, monthBounds } from '@/lib/format'
import { useFlow } from '@/lib/queries'
import { cn } from '@/lib/utils'

export function MonthFlow({ className }: { className?: string }) {
  const month = currentMonth()
  const { from, to } = monthBounds(month)
  const flow = useFlow({ from, to, granularity: 'month' })

  const point = flow.data?.points.find((p) => p.day.startsWith(month))
  const inflow = Math.max(0, point?.in.cents ?? 0)
  const outflow = Math.abs(point?.out.cents ?? 0)
  const max = Math.max(inflow, outflow)

  const rows = [
    { label: 'Entradas', cents: inflow, fill: 'bg-positive' },
    { label: 'Salidas', cents: outflow, fill: 'bg-chart-out' },
  ]

  return (
    <SectionCard title="Este mes" className={className}>
      {flow.isError ? (
        <p className="text-body text-pretty text-muted-foreground">
          No pudimos cargar este mes. Intenta otra vez en un momento.
        </p>
      ) : (
        <dl className="grid grid-cols-2 gap-4 lg:grid-cols-1 lg:gap-5">
          {rows.map((row) => {
            const share = max > 0 ? row.cents / max : 0
            return (
              <div
                key={row.label}
                className="grid min-w-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-baseline lg:gap-x-3"
              >
                <dt className="text-small font-medium text-muted-foreground lg:text-body lg:text-foreground">
                  {row.label}
                </dt>
                <dd className="lg:text-right">
                  {flow.isLoading ? (
                    <Skeleton className="my-1 h-4 w-24 rounded-full" />
                  ) : (
                    <Amount amount={row.cents} />
                  )}
                </dd>
                <dd
                  aria-hidden="true"
                  className="col-span-full mt-2 h-3 overflow-hidden rounded-full bg-muted"
                >
                  <span
                    className={cn(
                      'block h-3 rounded-full',
                      row.fill,
                      row.cents > 0 && 'min-w-3',
                    )}
                    style={{ width: `${share * 100}%` }}
                  />
                </dd>
              </div>
            )
          })}
        </dl>
      )}
    </SectionCard>
  )
}
