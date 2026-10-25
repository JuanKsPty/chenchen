/**
 * The month Gastos shows, and how its name reads in a sentence.
 *
 *   spendingPeriod(requested?)  { month, from, to, current } for ?mes. A missing,
 *                               malformed or future month falls back to this one.
 *                               `to` stops at today in the current month, so a link
 *                               into Movimientos reads "01–24 oct", not "01–31 oct".
 *   monthName("2026-09")        "Septiembre"
 *   monthShort("2026-09")       "Sep", the chart's axis label
 *   monthInSentence("2026-10")  "octubre"; "diciembre de 2025" in another year
 */
import { currentMonth, monthBounds, monthTitle, todayCivil } from '@/lib/format'

export interface SpendingPeriod {
  /** YYYY-MM */
  month: string
  /** First day, YYYY-MM-DD. */
  from: string
  /** Last day with movements to show: the month's end, or today in this month. */
  to: string
  /** The month that is still running. */
  current: boolean
}

export function spendingPeriod(requested?: string): SpendingPeriod {
  const now = currentMonth()
  const month = requested && requested <= now ? requested : now
  const current = month === now
  const bounds = monthBounds(month)
  return { month, from: bounds.from, to: current ? todayCivil() : bounds.to, current }
}

export function monthName(ym: string): string {
  return monthTitle(ym).split(' ')[0] ?? ym
}

export function monthShort(ym: string): string {
  return monthName(ym).slice(0, 3)
}

export function monthInSentence(ym: string): string {
  const name = monthName(ym).toLowerCase()
  const year = ym.slice(0, 4)
  return year === currentMonth().slice(0, 4) ? name : `${name} de ${year}`
}
