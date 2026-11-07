/**
 * What the plan says, in one place, so the plan page and Gastos' card never word the
 * same figure two ways.
 *
 *   headline(plan)        { label, figure, track }: "Te quedan" and what is left, or
 *                         the label that says which side of it the month is on
 *   shortfall(plan)       why nothing is free, when nothing is
 *   spentLine(plan)       "Llevas $35.63 de $580.00 libres." / "Usaste …"
 *   daysLine(plan)        "24 días · unos $22 al día", "Quedan 3 días del mes."
 *   fixedStatus(line, plan)  "Salió el 04 nov", "Salió el 18 oct · planeaste $3.50",
 *                         "Sale el 18 nov", "Aún no lo vemos · era el 05 nov", "Lo
 *                         pagaste desde otra cuenta"
 *   incomeStatus(line, plan) "Entró el 16 nov", "Llega el 30 nov", "Aún no lo vemos ·
 *                         era el 15 nov", "Te llegó a otra cuenta"
 *   monthWord(ym)         "noviembre" (or "enero de 2027"), for a sentence
 *   scopeNote(plan)       "Vale desde diciembre. Noviembre no cambia." for next month
 *
 * A figure below zero is never written "−$32.40": the label changes instead ("Por
 * encima de lo libre"), and the figure is how far. Nothing here scolds, warns or
 * exclaims: spending is not an error (spec voice).
 */
import type { ReactNode } from 'react'

import type { MonthPlan, PlanFixedLine, PlanIncomeLine } from '@/api/types'
import { Amount } from '@/components/kit'
import { monthInSentence, monthName } from '@/components/spending/period'
import { aboutMoney, formatCivilDayMonth, money, shiftMonth } from '@/lib/format'

export const monthWord = monthInSentence

export interface Headline {
  /** "Te quedan", "Por encima de lo libre", "Te sobraron" or "Libre para gastar". */
  label: string
  /** Never below zero: the label says which side of what is free it is on. */
  figure: number
  /** Whether there is anything free to draw what went out against. */
  track: boolean
}

export function headline(plan: MonthPlan): Headline {
  const { free, left } = plan.summary
  if (free.cents <= 0) return { label: 'Libre para gastar', figure: 0, track: false }
  if (plan.state === 'future')
    return { label: 'Libre para gastar', figure: free.cents, track: false }
  if (left.cents < 0)
    return { label: 'Por encima de lo libre', figure: -left.cents, track: true }
  if (plan.state === 'past')
    return { label: 'Te sobraron', figure: left.cents, track: true }
  return { label: 'Te quedan', figure: left.cents, track: true }
}

/** Why nothing is free: the plan's own shape, said plainly. */
export function shortfall(plan: MonthPlan): string {
  const { income, free } = plan.summary
  if (income.cents <= 0)
    return 'Agrega lo que te entra al mes y te decimos cuánto queda libre.'
  if (free.cents < 0)
    return `Tus pagos y tu ahorro suman ${money(-free.cents)} más que tu ingreso.`
  return 'Tus pagos y tu ahorro suman lo mismo que tu ingreso.'
}

/** "Llevas $35.63 de $580.00 libres." this month, "Usaste …" for one that closed. */
export function spentLine(plan: MonthPlan, { short = false } = {}): ReactNode {
  const { spent, free, left } = plan.summary
  if (plan.state === 'current' && left.cents === 0 && spent.cents > 0)
    return 'Usaste justo lo libre.'
  const verb = plan.state === 'past' ? 'Usaste' : 'Llevas'
  return (
    <>
      {verb} <Amount amount={Math.max(0, spent.cents)} size="inline" /> de{' '}
      <Amount amount={free} size="inline" />
      {short ? '' : ' libres.'}
    </>
  )
}

/** How many days are left, and roughly how much a day while something is. */
export function daysLine(plan: MonthPlan): string | null {
  const { days_left: days, per_day: perDay, left } = plan.summary
  if (plan.state === 'future') return perDay ? `Unos ${aboutMoney(perDay)} al día.` : null
  if (plan.state !== 'current' || days <= 0) return null
  if (days === 1) return 'Último día del mes.'
  if (left.cents <= 0 || !perDay) return `Quedan ${days} días del mes.`
  return perDay.cents < 100
    ? `${days} días · menos de $1 al día`
    : `${days} días · unos ${aboutMoney(perDay)} al día`
}

/** "unos $22 al día", the short form for a card; null when nothing is left. */
export function perDayWords(plan: MonthPlan): string | null {
  const perDay = plan.summary.per_day
  if (plan.state !== 'current' || !perDay) return null
  return perDay.cents < 100 ? 'menos de $1 al día' : `unos ${aboutMoney(perDay)} al día`
}

/** The first day of a month, "01 dic", for "Empieza el 01 dic." */
export function monthStart(ym: string): string {
  return formatCivilDayMonth(`${ym}-01`)
}

/**
 * Where a fixed payment stands this month, from the server's status: when it went
 * out, with what was planned when what went out differs from it (the row's figure is
 * what went out, so the two read side by side; `short` leaves the plan out, for a
 * sheet that leads with it); paid where chenchén cannot see, as the owner said; when
 * it goes out; or that it has not been seen.
 *
 * A movement recognised as the payment says more than the owner's mark that it was
 * paid elsewhere: it says when, and what it cost, which is what the month counts. So
 * the mark is the words only while no movement paid it.
 */
export function fixedStatus(
  line: PlanFixedLine,
  plan: MonthPlan,
  { short = false } = {},
): string {
  const due = formatCivilDayMonth(line.due_on)
  if (line.marked_paid && line.paid === null) return 'Lo pagaste desde otra cuenta'
  switch (line.status) {
    case 'out': {
      const first = [...line.movements]
        .filter((m) => m.amount.cents < 0)
        .sort((a, b) => a.booked_on.localeCompare(b.booked_on))[0]
      const when = first ? `Salió el ${formatCivilDayMonth(first.booked_on)}` : 'Ya salió'
      return !short && line.paid && line.paid.cents !== line.amount.cents
        ? `${when} · planeaste ${money(line.amount)}`
        : when
    }
    case 'late':
      return `Aún no lo vemos · era el ${due}`
    case 'missed':
      return `No lo vimos · era el ${due}`
    default:
      return line.due_on === plan.today ? 'Sale hoy' : `Sale el ${due}`
  }
}

/**
 * Where a paycheck stands this month, from the server's status: when it came in;
 * come in where chenchén cannot see, as the owner said, while no deposit is
 * recognised as it (one that is says more); when it is due; or that it has not been
 * seen. A paycheck may come in a few days either side of its day, even in the month
 * next door, and still be this month's.
 */
export function incomeStatus(line: PlanIncomeLine, plan: MonthPlan): string {
  const due = formatCivilDayMonth(line.due_on)
  if (line.marked_paid && line.movements.length === 0) return 'Te llegó a otra cuenta'
  switch (line.status) {
    case 'done': {
      const first = [...line.movements]
        .filter((m) => m.amount.cents > 0)
        .sort((a, b) => a.booked_on.localeCompare(b.booked_on))[0]
      return first ? `Entró el ${formatCivilDayMonth(first.booked_on)}` : 'Ya entró'
    }
    case 'late':
      return `Aún no lo vemos · era el ${due}`
    case 'missed':
      return `No lo vimos · era el ${due}`
    default:
      return line.due_on === plan.today ? 'Llega hoy' : `Llega el ${due}`
  }
}

/** For a dialog on next month: the edit starts there, this month stays. */
export function scopeNote(plan: MonthPlan): string | undefined {
  if (plan.state !== 'future') return undefined
  return `Vale desde ${monthWord(plan.month)}. ${monthName(shiftMonth(plan.month, -1))} no cambia.`
}
