/**
 * <PlanGlance className? />
 *
 * Inicio's look at this month's plan, right after "En qué se te va": "Plan de
 * noviembre" with "Ver plan", then "Te quedan", the figure, "unos $22 al día" and
 * the track. Past what is free, the label changes and the figure is how far, in ink
 * and unsigned ("Por encima de lo libre", "Llevas $612.40 de $580.00 libres."), never
 * a negative figure; a plan that leaves nothing free says why, as the plan's page
 * does. The words are the plan page's own (words.tsx).
 *
 * The figure is a heading: Patrimonio keeps Inicio's one display figure and its one
 * solid veranera block. It comes after the first list, so Inicio still shows at
 * most five figures before it.
 *
 * Nothing at all while the plan loads, when it fails, or when the month has no
 * plan: Inicio cannot know there is one to show until it arrives, and a skeleton
 * that then vanishes would jump the page. The plan is asked for without a month,
 * the same entry as the plan page's and Gastos' card, and the server (Panama time)
 * says which month it is.
 */
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { Amount, PlanTrack, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { PLAN_PATH } from '@/lib/nav'
import { usePlan } from './usePlan'
import { headline, monthWord, perDayWords, shortfall, spentLine } from './words'

export function PlanGlance({ className }: { className?: string }) {
  const plan = usePlan()
  // From the data, not the status: a failed refresh keeps what was already shown.
  const data = plan.data
  if (!data?.planned) return null

  const head = headline(data)
  let line: ReactNode
  if (data.summary.free.cents <= 0) line = shortfall(data)
  else if (head.label === 'Te quedan') line = perDayWords(data) ?? spentLine(data)
  else line = spentLine(data)

  return (
    <SectionCard
      title={`Plan de ${monthWord(data.month)}`}
      action={
        <Button asChild variant="ghost">
          <Link to={PLAN_PATH} aria-label="Ver plan del mes">
            Ver plan
          </Link>
        </Button>
      }
      className={className}
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
