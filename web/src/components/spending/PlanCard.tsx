/**
 * <PlanCard period />
 *
 * Gastos' way into the plan, right after "Salidas de octubre". OutflowCard owns the
 * view's display figure, so this one's is a heading.
 *   - This month, planned: "Plan de noviembre" with "Ver plan"; "Te quedan", the
 *     figure, "Llevas $35.63 de $580.00 · unos $22 al día" and the track. Past what
 *     was free, "Por encima de lo libre" and how far, never a negative figure.
 *   - A past month, planned: "Te sobraron" (or "Por encima de lo libre") and "Usaste
 *     $536.80 de $580.00", linking to that month's plan.
 *   - This month, nothing planned: an invite, "Armar plan", which opens the setup on
 *     arrival.
 *   - A past month with nothing planned: nothing.
 *
 * "Llevas" is not "Salidas": it leaves out what the fixed payments took. The plan's
 * page says so; here the words differ so the two figures are not read as one.
 *
 * This month is asked for without a month, the same entry as the plan page's. While
 * it loads this month shows a skeleton; a past month shows nothing until it knows
 * there is a plan. On a failed load this month says so (LoadError) and a past month
 * stays away: it may never have had a plan.
 */
import { Link } from 'react-router-dom'

import { Amount, PlanTrack, SectionCard } from '@/components/kit'
import { usePlan } from '@/components/plan/usePlan'
import { headline, perDayWords, shortfall, spentLine } from '@/components/plan/words'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { planHref } from '@/lib/nav'
import { LoadError } from './LoadError'
import { monthInSentence, type SpendingPeriod } from './period'

export function PlanCard({ period }: { period: SpendingPeriod }) {
  const plan = usePlan(period.current ? undefined : period.month)
  const data = plan.data
  const title = `Plan de ${monthInSentence(period.month)}`
  const href = planHref(period.current ? undefined : period.month)

  if (!data) {
    if (!period.current) return null
    return (
      <SectionCard title={title}>
        {plan.isPending ? (
          <div className="flex flex-col gap-2" aria-hidden="true">
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-7 w-36" />
            <Skeleton className="h-4 w-56" />
            <Skeleton className="h-2" />
          </div>
        ) : (
          <LoadError />
        )}
      </SectionCard>
    )
  }

  if (!data.planned) {
    if (!period.current) return null
    return (
      <SectionCard
        title="Plan del mes"
        action={
          <Button asChild variant="ghost">
            <Link to={planHref(undefined, { setup: true })}>Armar plan</Link>
          </Button>
        }
      >
        <p className="text-body text-pretty text-muted-foreground">
          Arma tu plan y te decimos cuánto te queda libre cada día.
        </p>
      </SectionCard>
    )
  }

  const head = headline(data)
  const perDay = head.label === 'Te quedan' ? perDayWords(data) : null
  const spent = spentLine(data, { short: true })
  // A sentence of its own ("Usaste justo lo libre.") already ends.
  const line =
    data.summary.free.cents <= 0 ? (
      shortfall(data)
    ) : typeof spent === 'string' ? (
      spent
    ) : (
      <>
        {spent}
        {perDay ? ` · ${perDay}` : '.'}
      </>
    )

  return (
    <SectionCard
      title={title}
      action={
        <Button asChild variant="ghost">
          <Link to={href} aria-label="Ver plan del mes">
            Ver plan
          </Link>
        </Button>
      }
    >
      <div className="flex flex-col gap-2">
        <p className="flex flex-col">
          <span className="text-small font-medium text-muted-foreground">{head.label}</span>
          <Amount amount={head.figure} size="heading" />
        </p>
        <p className="text-small font-medium text-pretty text-muted-foreground">{line}</p>
        {head.track && <PlanTrack amount={data.summary.spent} cap={data.summary.free} />}
      </div>
    </SectionCard>
  )
}
