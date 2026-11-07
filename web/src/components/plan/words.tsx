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
 *   suggestionHint(s, {amount?, months?})
 *                         "$3.68 · cerca del día 18", "Cerca del día 18 · salió en 3
 *                         meses", "$300.00 al mes": what a suggestion says under its name
 *   seenIncome(income)    "Vimos $650.00 dos veces al mes, cerca del 16 y del 30.", from
 *                         the paychecks the history suggests; null with none
 *   freeLine(free, income)  "$1,061.02 libres al mes.", or why nothing would be
 *   purchaseStatus(line, plan)  "Marzo 2027 · apartas $220.00 al mes", "Este mes ·
 *                         $1,100.00 apartados en el plan", "La compraste el 12 mar ·
 *                         $1,050.00", "No la compraste en marzo"
 *   setAsideHint(funding, left, from, target)
 *                         "Apartas $220.00 al mes de noviembre a marzo.", "Apartas
 *                         $1,100.00 en marzo.", "No resta de lo libre: la pagas con tus
 *                         ahorros.": what a purchase will set aside, before it is saved
 *   savingStatus(line, plan, where)  "Este mes apartaste $150.00 de $200.00", "Ya
 *                         apartaste los $200.00 de este mes", "Aún no llega nada este
 *                         mes", "No llegó nada en octubre", "Sin cuenta: solo cuenta lo
 *                         que planeas"
 *   goalNeeded(line)      "Para marzo te faltan unos $550 al mes"
 *
 * A purchase's money is set aside in the plan only: "apartado en el plan", never a
 * balance, and no account is said to hold it. A saving's progress is what its account
 * holds, as Cuentas shows it.
 *
 * A figure below zero is never written "−$32.40": the label changes instead ("Por
 * encima de lo libre"), and the figure is how far. Nothing here scolds, warns or
 * exclaims: spending is not an error (spec voice).
 */
import type { ReactNode } from 'react'

import type {
  MonthPlan,
  PlanFixedLine,
  PlanFunding,
  PlanIncomeLine,
  PlanPurchaseLine,
  PlanSavingLine,
  PlanSuggestion,
} from '@/api/types'
import { Amount } from '@/components/kit'
import { monthInSentence, monthName } from '@/components/spending/period'
import {
  aboutMoney,
  formatCivilDayMonth,
  money,
  monthTitle,
  shiftMonth,
} from '@/lib/format'
import { purchaseShares, setAsideSoFar } from './shares'

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

/**
 * What a suggestion says under its name. A paycheck or a payment: its amount (left
 * out where the row shows it, or lets it be typed), "cerca del día 18", with
 * `months` in how many months it came, and, for a payment to or from a person, the
 * text the statement prints: two of them are both "Yappy de cada mes", and that text
 * is what tells them apart. It is the owner's own statement, said as data; chenchén
 * never makes a name of it. A saving: what it would set aside, "$300.00 al mes".
 */
export function suggestionHint(
  s: PlanSuggestion,
  { amount = true, months = false } = {},
): string {
  if (s.kind === 'saving') return `${money(s.amount)} al mes`
  const parts: string[] = []
  if (amount) parts.push(money(s.amount))
  if (s.day) parts.push(`cerca del día ${s.day}`)
  if (months)
    parts.push(`${s.kind === 'income' ? 'llegó' : 'salió'} en ${s.months_seen} meses`)
  if (s.person && s.match_text.trim()) parts.push(s.match_text.trim())
  const text = parts.join(' · ')
  return text.charAt(0).toUpperCase() + text.slice(1)
}

/** A suggested paycheck's key without its place in the month ("#1", "#2"). */
const paycheckGroup = (key: string) => key.replace(/#[12]$/, '')

/**
 * What the history says about income, figure first: "Vimos $650.00 dos veces al
 * mes, cerca del 16 y del 30." A paycheck that comes twice a month is two
 * suggestions, one per place ("#1", "#2"), said here as one; another paycheck adds
 * "También $40.00 al mes, cerca del 5." Null when nothing was suggested.
 */
export function seenIncome(income: PlanSuggestion[]): string | null {
  const groups = new Map<string, PlanSuggestion[]>()
  for (const s of income) {
    const group = paycheckGroup(s.key)
    groups.set(group, [...(groups.get(group) ?? []), s])
  }
  const near = (s: PlanSuggestion) => (s.day ? `cerca del ${s.day}` : '')
  const said = [...groups.values()].map((group) => {
    const [first, second] = [...group].sort((a, b) => (a.day ?? 0) - (b.day ?? 0))
    if (!first) return ''
    if (!second)
      return [`${money(first.amount)} al mes`, near(first)].filter(Boolean).join(', ')
    if (first.amount.cents === second.amount.cents)
      return `${money(first.amount)} dos veces al mes, cerca del ${first.day} y del ${second.day}`
    return `${money(first.amount)} ${near(first)} y ${money(second.amount)} ${near(second)} de cada mes`
  })
  const [lead, ...rest] = said.filter(Boolean)
  if (!lead) return null
  return [`Vimos ${lead}.`, ...rest.map((clause) => `También ${clause}.`)].join(' ')
}

/**
 * What a plan of `income` a month would leave free, `free`, figure first: "$1,061.02
 * libres al mes." Never a figure below zero: when what goes out and what is set aside
 * add up to more than the income it says by how much, plainly.
 */
export function freeLine(free: number, income: number): ReactNode {
  if (income <= 0) return 'Dinos cuánto te entra y te decimos cuánto queda libre.'
  if (free < 0) return `Tus pagos y tu ahorro suman ${money(-free)} más que tu ingreso.`
  if (free === 0) return 'Tus pagos y tu ahorro suman lo mismo que tu ingreso.'
  return (
    <>
      <Amount amount={free} size="inline" /> libres al mes.
    </>
  )
}

/**
 * Where a planned purchase stands in the month read, from the server's status. Every
 * figure is the plan's: what it sets aside, never what an account holds.
 *   - still being set aside: its month, and this month's share ("Marzo 2027 · apartas
 *     $220.00 al mes"); all of it in its month ("Marzo 2027 · apartas todo ese mes");
 *     or paid with savings ("Marzo 2027 · con tus ahorros")
 *   - due this month: what the plan set aside for it by now ("Este mes · $1,100.00
 *     apartados en el plan"), or, paid with savings, "Este mes · con tus ahorros"
 *   - bought: the day, and what it cost when that is not its price, which the row
 *     shows beside it ("La compraste el 12 mar · $1,050.00")
 *   - its month ended without it: "No la compraste en marzo", plainly
 */
export function purchaseStatus(line: PlanPurchaseLine, plan: MonthPlan): string {
  switch (line.status) {
    case 'bought': {
      const when = line.bought_on
        ? `La compraste el ${formatCivilDayMonth(line.bought_on)}`
        : 'Ya la compraste'
      return line.paid && line.paid.cents !== line.amount.cents
        ? `${when} · ${money(line.paid)}`
        : when
    }
    case 'missed':
      return `No la compraste en ${monthWord(line.target_month)}`
    case 'this_month': {
      const when = plan.state === 'current' ? 'Este mes' : `En ${monthWord(plan.month)}`
      if (line.funding === 'savings') return `${when} · con tus ahorros`
      return `${when} · ${money(setAsideSoFar(line))} apartados en el plan`
    }
    default: {
      const when = monthTitle(line.target_month)
      if (line.funding === 'savings') return `${when} · con tus ahorros`
      if (line.funding === 'that_month') return `${when} · apartas todo ese mes`
      return line.share.cents > 0
        ? `${when} · apartas ${money(line.share)} al mes`
        : `${when} · ya está apartada en el plan`
    }
  }
}

/**
 * What a purchase would set aside, said before it is saved, from the shares the
 * server will write (purchaseShares): `left` of the price spread over the months from
 * `from` to `target`, all of it in `target`, or nothing with savings. Null until
 * there is a price and a month to say it of. Shares that differ by a cent say both,
 * so the hint never rounds what the plan will hold.
 */
export function setAsideHint(
  funding: PlanFunding,
  left: number | null,
  from: string,
  target: string,
): string | null {
  if (funding === 'savings') return 'No resta de lo libre: la pagas con tus ahorros.'
  if (left === null || !target) return null
  if (left <= 0) return 'Ya está apartada en el plan.'
  const shares = funding === 'monthly' ? purchaseShares(left, from, target) : [left]
  if (shares.length <= 1) return `Apartas ${money(left)} en ${monthWord(target)}.`
  const low = Math.min(...shares)
  const high = Math.max(...shares)
  const each = low === high ? money(low) : `${money(low)} o ${money(high)}`
  return `Apartas ${each} al mes de ${monthWord(from)} a ${monthWord(target)}.`
}

/**
 * Where a saving stands in the month read, from what reached its account as
 * transfers (`saved_month`) against what the month sets aside. Next month nothing has
 * arrived yet, so it says where the money goes instead (`where`, "Al mes, en
 * Inversión"). With no account the plan cannot tell, and says so.
 */
export function savingStatus(line: PlanSavingLine, plan: MonthPlan, where: string): string {
  const planned = line.amount.cents
  const saved = line.saved_month?.cents ?? 0
  const past = plan.state === 'past'
  const month = monthWord(plan.month)
  if (line.status === 'unlinked') return 'Sin cuenta: solo cuenta lo que planeas'
  if (plan.state === 'future') return where
  switch (line.status) {
    case 'done':
      if (saved > planned) {
        const more = money(saved - planned)
        return past
          ? `En ${month} apartaste ${money(saved)} · ${more} más de lo previsto`
          : `Este mes apartaste ${money(saved)} · ${more} más de lo previsto`
      }
      return past
        ? `Apartaste los ${money(planned)} de ${month}`
        : `Ya apartaste los ${money(planned)} de este mes`
    case 'partial':
      return past
        ? `En ${month} apartaste ${money(saved)} de ${money(planned)}`
        : `Este mes apartaste ${money(saved)} de ${money(planned)}`
    case 'missed':
      return `No llegó nada en ${month}`
    default:
      return 'Aún no llega nada este mes'
  }
}

/**
 * What each month to a goal's date would have to set aside to reach it, roughly:
 * "Para marzo te faltan unos $550 al mes". Null without a goal and a date, or once
 * it is reached.
 */
export function goalNeeded(line: PlanSavingLine): string | null {
  const needed = line.needed_per_month
  if (!needed || !line.target_month || needed.cents <= 0) return null
  const when = monthWord(line.target_month)
  return needed.cents < 100
    ? `Para ${when} te falta menos de $1 al mes`
    : `Para ${when} te faltan unos ${aboutMoney(needed)} al mes`
}
