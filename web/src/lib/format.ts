import type { Account, Amount, EntryKind } from '@/api/types'

/**
 * Rendering money and dates.
 *
 * Every amount is formatted from integer cents. No float ever holds a monetary
 * value: `Intl.NumberFormat` is handed cents/100 only at the last moment.
 *
 * Money:
 *   money(amount)        "$1,234.56", and "−$84.37" for a negative (U+2212, never a hyphen)
 *   signedMoney(amount)  "+$3,800.00" / "−$84.37" / "$0.00": a movement, sign glued to the $
 *   moneyParts(amount, {signed}?)  { whole: "$84,213", cents: ".25" } for smaller cents
 *   moneyFromText("100.00")  a decimal string the API sent as text
 *
 * Civil dates (YYYY-MM-DD, no zone; always built from their parts so a Panamanian
 * browser never shows the day before):
 *   formatCivilDate("2026-10-03")      "03 oct 2026"
 *   formatCivilDayMonth("2026-10-03")  "03 oct"
 *   dayHeading("2026-10-24")           "Sábado 24 oct" (plus the year when it is not this year)
 *   monthTitle("2026-10" | "2026-10-24")  "Octubre 2026"
 *   formatCivilMonth(ymd)              same as monthTitle, kept for older callers
 *   formatRange(from?, to?)            "01–24 oct", "28 sept – 03 oct", "Desde 01 oct"
 *   todayCivil()                       today as YYYY-MM-DD
 *   currentMonth()                     this month as YYYY-MM
 *   shiftMonth("2026-10", -1)          "2026-09"
 *   monthBounds("2026-10")             { from: "2026-10-01", to: "2026-10-31" }
 *
 * Instants (ISO timestamps):
 *   formatDate(iso)       "24 oct 2026"
 *   relativeTime(iso)     "hace un momento", "hace 5 min", "hace 2 h", "ayer",
 *                         "hace 3 días", "el 05 oct"
 *
 * Accounts and movements:
 *   accountLabel(a), accountTypeLabel(type), institutionLabel(institution),
 *   numberShort(number) "···4417", "U···567", entryKindLabel(kind).
 *   The short labels built from these ("<banco> Corriente") live in lib/account.ts.
 */

const currency = new Intl.NumberFormat('es-PA', {
  style: 'currency',
  currency: 'USD',
  // Without this the es-PA locale writes the ISO code ("USD 85,047.62").
  currencyDisplay: 'narrowSymbol',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})

/** The real minus sign. A hyphen is narrower than the digits and misaligns columns. */
export const MINUS = '−'

function centsOf(amount: Amount | number): number {
  return typeof amount === 'number' ? amount : amount.cents
}

/** "$1,234.56"; a negative amount gets "−" glued to the "$". */
export function money(amount: Amount | number): string {
  const cents = centsOf(amount)
  const formatted = currency.format(Math.abs(cents) / 100)
  return cents < 0 ? `${MINUS}${formatted}` : formatted
}

/** A movement: "+$3,800.00" in, "−$84.37" out, "$0.00" for nothing. */
export function signedMoney(amount: Amount | number): string {
  const cents = centsOf(amount)
  const formatted = currency.format(Math.abs(cents) / 100)
  if (cents > 0) return `+${formatted}`
  if (cents < 0) return `${MINUS}${formatted}`
  return formatted
}

/** Formats a decimal string the API gave as text, e.g. the chat's "100.00". */
export function moneyFromText(text: string): string {
  const parsed = Number.parseFloat(text)
  return Number.isFinite(parsed) ? money(Math.round(parsed * 100)) : text
}

/** Splits an amount so the cents can be set smaller than the dollars. */
export function moneyParts(
  amount: Amount | number,
  options: { signed?: boolean } = {},
): { whole: string; cents: string } {
  const formatted = options.signed ? signedMoney(amount) : money(amount)
  const separator = formatted.lastIndexOf('.')
  if (separator === -1) return { whole: formatted, cents: '' }
  return { whole: formatted.slice(0, separator), cents: formatted.slice(separator) }
}

// --- dates -----------------------------------------------------------------

const dateFormat = new Intl.DateTimeFormat('es', {
  day: '2-digit',
  month: 'short',
  year: 'numeric',
})
const dayMonthFormat = new Intl.DateTimeFormat('es', { day: '2-digit', month: 'short' })
const monthNameFormat = new Intl.DateTimeFormat('es', { month: 'long' })
const weekdayFormat = new Intl.DateTimeFormat('es', { weekday: 'long' })

function capitalize(text: string): string {
  return `${text.charAt(0).toUpperCase()}${text.slice(1)}`
}

/** A civil date as a local Date at midnight, so no zone can move the day. */
function civil(ymd: string): Date {
  const [year = 0, month = 1, day = 1] = ymd.split('-').map(Number)
  return new Date(year, month - 1, day)
}

function ymdOf(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

/** "24 oct 2026" for an instant. */
export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso))
}

/** "03 oct 2026" for a civil date. */
export function formatCivilDate(ymd: string): string {
  return dateFormat.format(civil(ymd))
}

/** "03 oct" for a civil date. */
export function formatCivilDayMonth(ymd: string): string {
  return dayMonthFormat.format(civil(ymd))
}

/**
 * "Sábado 24 oct": the heading over a day's movements. The year is added only
 * when it is not the current one ("Martes 30 dic 2025").
 */
export function dayHeading(ymd: string): string {
  const date = civil(ymd)
  const heading = `${capitalize(weekdayFormat.format(date))} ${dayMonthFormat.format(date)}`
  return date.getFullYear() === new Date().getFullYear()
    ? heading
    : `${heading} ${date.getFullYear()}`
}

/** "Octubre 2026" for "2026-10" or any day in it. Not "octubre de 2026": a title is not a sentence. */
export function monthTitle(ymOrYmd: string): string {
  const [year = 0, month = 1] = ymOrYmd.split('-').map(Number)
  return `${capitalize(monthNameFormat.format(new Date(year, month - 1, 1)))} ${year}`
}

/** "Junio 2026" for a civil date's month. Same as `monthTitle`. */
export function formatCivilMonth(ymd: string): string {
  return monthTitle(ymd)
}

/** Today in the browser's own calendar, as YYYY-MM-DD. */
export function todayCivil(): string {
  return ymdOf(new Date())
}

/** This month, as YYYY-MM. */
export function currentMonth(): string {
  return todayCivil().slice(0, 7)
}

/** The month `delta` months away from `ym` (YYYY-MM). */
export function shiftMonth(ym: string, delta: number): string {
  const [year = 0, month = 1] = ym.split('-').map(Number)
  return ymdOf(new Date(year, month - 1 + delta, 1)).slice(0, 7)
}

/** The first and last civil day of a month (YYYY-MM), for report and movement filters. */
export function monthBounds(ym: string): { from: string; to: string } {
  const [year = 0, month = 1] = ym.split('-').map(Number)
  return {
    from: ymdOf(new Date(year, month - 1, 1)),
    to: ymdOf(new Date(year, month, 0)),
  }
}

/**
 * A date range for a filter chip: "01–24 oct", "28 sept – 03 oct",
 * "15 dic 2025 – 10 ene 2026", "Desde 01 oct", "Hasta 24 oct".
 */
export function formatRange(from?: string, to?: string): string {
  const thisYear = new Date().getFullYear()
  const short = (ymd: string) =>
    civil(ymd).getFullYear() === thisYear ? formatCivilDayMonth(ymd) : formatCivilDate(ymd)
  if (from && to) {
    if (from === to) return short(from)
    const a = civil(from)
    const b = civil(to)
    if (a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth()) {
      return `${String(a.getDate()).padStart(2, '0')}–${short(to)}`
    }
    if (a.getFullYear() === b.getFullYear() && a.getFullYear() === thisYear) {
      return `${formatCivilDayMonth(from)} – ${formatCivilDayMonth(to)}`
    }
    return `${formatCivilDate(from)} – ${formatCivilDate(to)}`
  }
  if (from) return `Desde ${short(from)}`
  if (to) return `Hasta ${short(to)}`
  return ''
}

const MINUTE = 60_000
const HOUR = 60 * MINUTE

/**
 * How long ago an instant was, in the words a person uses: "hace 2 h", "ayer".
 * Reads after a verb: "Sincronizado hace 2 h", "Sincronizado el 05 oct".
 */
export function relativeTime(iso: string, now: Date = new Date()): string {
  const then = new Date(iso)
  const elapsed = now.getTime() - then.getTime()
  if (elapsed < MINUTE) return 'hace un momento'
  if (elapsed < HOUR) return `hace ${Math.floor(elapsed / MINUTE)} min`

  const today = civil(ymdOf(now))
  const day = civil(ymdOf(then))
  const days = Math.round((today.getTime() - day.getTime()) / (24 * HOUR))

  if (elapsed < 12 * HOUR || days === 0) return `hace ${Math.floor(elapsed / HOUR)} h`
  if (days === 1) return 'ayer'
  if (days < 7) return `hace ${days} días`
  return then.getFullYear() === now.getFullYear()
    ? `el ${dayMonthFormat.format(then)}`
    : `el ${dateFormat.format(then)}`
}

// --- accounts and movements -------------------------------------------------

const ACCOUNT_TYPES: Record<string, string> = {
  checking: 'Cuenta corriente',
  savings: 'Cuenta de ahorros',
  credit_card: 'Tarjeta de crédito',
  brokerage: 'Inversiones',
}

export function accountTypeLabel(type: string): string {
  return ACCOUNT_TYPES[type] ?? type
}

const INSTITUTIONS: Record<string, string> = {
  banco_general: 'Banco General',
  bac: 'BAC',
  ibkr: 'Interactive Brokers',
}

export function institutionLabel(institution: string): string {
  return INSTITUTIONS[institution] ?? institution
}

/**
 * What to call an account on screen: the name its owner gave it, or the name its
 * bank gives it. The number is not folded in; callers show it separately.
 */
export function accountLabel(
  account: Pick<Account, 'alias' | 'display_name' | 'type'>,
): string {
  return account.alias?.trim() || account.display_name || accountTypeLabel(account.type)
}

/**
 * The end of an account number, as a person tells accounts apart: "···4417" for a
 * bank account or a card, "U···567" for an IBKR account. A letter prefix is kept,
 * because it says which kind of account it is.
 */
export function numberShort(number: string): string {
  const prefix = /^[A-Za-z]+/.exec(number.trim())?.[0]
  const digits = number.replace(/[^0-9]/g, '')
  if (prefix && digits.length >= 3) return `${prefix.toUpperCase()}···${digits.slice(-3)}`
  return digits.length >= 4 ? `···${digits.slice(-4)}` : number
}

const ENTRY_KINDS: Record<EntryKind, string> = {
  income: 'Entrada',
  expense: 'Gasto',
  refund: 'Reembolso',
  fee: 'Comisión',
  interest: 'Intereses',
  transfer: 'Transferencia entre tus cuentas',
  trade: 'Compraventa de valores',
}

export function entryKindLabel(kind: EntryKind): string {
  return ENTRY_KINDS[kind] ?? kind
}
