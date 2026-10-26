/**
 * <MonthChart months selected partial? className? />
 *
 *   months    [{ month: 'YYYY-MM', in: cents, out: cents }], oldest first (four)
 *   selected  the month the page is about; its axis label is in primary-text 600
 *   partial   true while that month is still running: the table says "(al 24)"
 *
 * Spec monthChart: "Entradas y salidas" with a legend of 10px dots, grouped vertical
 * bars per month (16 wide, 4 apart, pill tops, 160 tall at the top of the scale),
 * entradas in `positive` and salidas in `chart-out`, never red. The scale rounds the
 * largest figure up to 1, 2, 2.5 or 5 × 10ⁿ ($5,000 for the brief's months) with
 * three gridlines. CSS only. The picture is aria-hidden; a visually hidden table
 * carries the same eight figures. With nothing in or out in all four months (before
 * the first statement), a short line replaces the empty plot.
 */
import { Amount } from '@/components/kit'
import { money, todayCivil } from '@/lib/format'
import { cn } from '@/lib/utils'
import { monthName, monthShort } from './period'

export interface MonthFlow {
  month: string
  in: number
  out: number
}

/** The top of the scale, in cents: the largest figure rounded up to a round number. */
function scaleTop(cents: number): number {
  if (cents <= 0) return 0
  const dollars = cents / 100
  const power = 10 ** Math.floor(Math.log10(dollars))
  const step = [1, 2, 2.5, 5, 10].find((s) => s * power >= dollars) ?? 10
  return Math.round(step * power * 100)
}

/** "$5,000", "$2,500", "$0": an axis tick drops ".00". */
function tick(cents: number): string {
  return money(cents).replace(/\.00$/, '')
}

export function MonthChart({
  months,
  selected,
  partial = false,
  className,
}: {
  months: MonthFlow[]
  selected: string
  partial?: boolean
  className?: string
}) {
  const top = scaleTop(Math.max(0, ...months.flatMap((m) => [m.in, m.out])))
  const height = (cents: number) =>
    top > 0 && cents > 0 ? `${Math.max(2, (cents / top) * 100)}%` : '0%'
  const today = Number(todayCivil().slice(8, 10))

  // Months before the first statement: no bars, and an axis of "$0 / $0 / $0" says
  // nothing. A sentence does.
  if (top === 0) {
    return (
      <div className={cn('flex min-w-0 flex-col gap-4', className)}>
        <h3 className="text-small font-semibold text-muted-foreground">
          Entradas y salidas
        </h3>
        <p className="flex min-h-44 items-center justify-center rounded-2xl bg-muted p-4 text-center text-small font-medium text-pretty text-muted-foreground">
          No hay entradas ni salidas de{' '}
          {monthName(months[0]?.month ?? selected).toLowerCase()} a{' '}
          {monthName(selected).toLowerCase()}.
        </p>
      </div>
    )
  }

  return (
    <div className={cn('flex min-w-0 flex-col gap-4', className)}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <h3 className="text-small font-semibold text-muted-foreground">
          Entradas y salidas
        </h3>
        <div
          aria-hidden="true"
          className="flex gap-4 text-small font-medium text-muted-foreground"
        >
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2.5 rounded-full bg-positive" />
            Entradas
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2.5 rounded-full bg-chart-out" />
            Salidas
          </span>
        </div>
      </div>

      <div
        aria-hidden="true"
        className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2"
      >
        <div className="-my-2 flex h-44 flex-col justify-between text-right text-micro font-semibold text-muted-foreground">
          <span>{tick(top)}</span>
          <span>{tick(Math.round(top / 2))}</span>
          <span>{tick(0)}</span>
        </div>
        <div className="relative h-40">
          <span className="absolute inset-x-0 top-0 h-px bg-line" />
          <span className="absolute inset-x-0 top-1/2 h-px bg-line" />
          <span className="absolute inset-x-0 bottom-0 h-px bg-line" />
          <div className="absolute inset-0 flex items-end justify-around">
            {months.map((m) => (
              <div key={m.month} className="flex h-full w-9 items-end gap-1">
                <span
                  className="block w-4 rounded-t-full bg-positive"
                  style={{ height: height(m.in) }}
                />
                <span
                  className="block w-4 rounded-t-full bg-chart-out"
                  style={{ height: height(m.out) }}
                />
              </div>
            ))}
          </div>
        </div>
        <span />
        <div className="flex justify-around">
          {months.map((m) => (
            <span
              key={m.month}
              className={cn(
                'w-9 text-center text-micro font-semibold',
                m.month === selected ? 'text-primary-text' : 'text-muted-foreground',
              )}
            >
              {monthShort(m.month)}
            </span>
          ))}
        </div>
      </div>

      <table className="sr-only">
        <caption>Entradas y salidas por mes</caption>
        <thead>
          <tr>
            <th scope="col">Mes</th>
            <th scope="col">Entradas</th>
            <th scope="col">Salidas</th>
          </tr>
        </thead>
        <tbody>
          {months.map((m) => (
            <tr key={m.month}>
              <th scope="row">
                {monthName(m.month)}
                {partial && m.month === selected ? ` (al ${today})` : ''}
              </th>
              <td>
                <Amount amount={m.in} size="inline" />
              </td>
              <td>
                <Amount amount={m.out} size="inline" />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
