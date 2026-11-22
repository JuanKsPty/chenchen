/**
 * <MonthProjection plan className? />
 *
 * "A fin de mes": where the bank accounts are headed by the month's end, from the
 * server's projection. "Llegas al 30 con" and the figure; the lowest day when it
 * comes before the end ("Lo más bajo: −$450.00 el 14 nov."); the line day by day
 * (kit/TrendChart, a dotted zero when it crosses it), resting on the month's end;
 * then the next three things the plan expects, each with its day and signed amount.
 * Under them, the one assumption that is not on the list: what is free counts as
 * spent, a day's share each day.
 *
 * Nothing at all when the month has no projection: a past or future month, or a bank
 * account with movements and no starting balance, whose balance would be made up.
 * Below zero is said plainly, never in orange: it is a heads-up, not a reprimand.
 */
import type { MonthPlan } from '@/api/types'
import { Amount, SectionCard, TrendChart } from '@/components/kit'
import { aboutMoney, formatCivilDayMonth } from '@/lib/format'

const NEXT = 3

export function MonthProjection({
  plan,
  className,
}: {
  plan: MonthPlan
  className?: string
}) {
  const projection = plan.projection
  const end = projection?.days.at(-1)
  if (!projection || !end) return null

  const lowest = projection.lowest
  const lowestBefore = lowest.day !== end.day && lowest.balance.cents < end.balance.cents
  const next = projection.events.filter((e) => e.day >= plan.today).slice(0, NEXT)
  const lastDay = Number(end.day.slice(8))

  return (
    <SectionCard title="A fin de mes" className={className}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <p className="flex flex-col">
            <span className="text-small font-medium text-muted-foreground">
              Llegas al {lastDay} con
            </span>
            <Amount amount={projection.end} size="heading" />
          </p>
          {lowestBefore && (
            <p className="text-small font-medium text-pretty text-muted-foreground">
              Lo más bajo:{' '}
              <Amount amount={lowest.balance} size="inline" className="text-foreground" />{' '}
              {lowest.day === plan.today ? 'hoy' : `el ${formatCivilDayMonth(lowest.day)}`}.
            </p>
          )}
        </div>

        <TrendLine plan={plan} />

        {next.length > 0 && (
          <ul className="flex flex-col gap-2">
            {next.map((e) => (
              <li
                key={`${e.kind}-${e.item_id ?? e.account_id ?? e.name}-${e.day}`}
                className="flex items-baseline justify-between gap-3 text-body"
              >
                <span className="min-w-0 truncate">
                  {e.name}
                  <span className="text-small text-muted-foreground">
                    {' · '}
                    {e.day === plan.today ? 'hoy' : formatCivilDayMonth(e.day)}
                  </span>
                </span>
                <Amount amount={e.amount} signed size="inline" />
              </li>
            ))}
          </ul>
        )}

        {projection.free_per_day.cents > 0 && (
          <p className="text-small text-pretty text-muted-foreground">
            Cuenta lo libre como gastado: unos {aboutMoney(projection.free_per_day)} al día.
          </p>
        )}
      </div>
    </SectionCard>
  )
}

function TrendLine({ plan }: { plan: MonthPlan }) {
  const days = plan.projection?.days ?? []
  return (
    <TrendChart
      label="Saldo de tus cuentas, día a día hasta fin de mes"
      zeroLine
      points={days.map((d) => ({ day: d.day, cents: d.balance.cents, partial: false }))}
      pointLabel={(point, index) =>
        index === 0 ? 'Hoy' : `El ${formatCivilDayMonth(point.day)}`
      }
    />
  )
}
