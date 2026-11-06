import type { Account, Amount, Institution, ShortcutStatus } from '@/api/types'
import {
  formatCivilDate,
  institutionLabel,
  money,
  numberShort,
  relativeTime,
} from '@/lib/format'

/**
 * What the app says about an account, in one place.
 *
 *   amountOf(cents)            an Amount built from cents (a sum made in the browser)
 *   paidByOther(a)             whether it is a card someone else pays: its purchases
 *                              count in the owner's spending, never in what they owe,
 *                              their net worth or the accounts lacking a starting
 *                              balance
 *   lacksOpening(a)            movements but no starting balance, so the balance is
 *                              only a sum ("Sin saldo inicial"); never a card someone
 *                              else pays
 *   headline(a)                { amount, label }: the one figure an account is summed
 *                              up by. Este mes (this_month) for a card someone else
 *                              pays, Debes (owed) for any other card, Total (cash plus
 *                              positions) for a broker, Saldo otherwise.
 *   accountStatus(a)           { kind: 'cuadra', asOf }
 *                              | { kind: 'no-cuadra', difference, asOf, stated }
 *                              | { kind: 'sincronizado', at }
 *                              | { kind: 'sin-partida' }
 *                              | null (nothing worth a chip; always for a card
 *                              someone else pays)
 *   reconciliationNote(a)      { text, tone: 'ok' | 'warn' | 'none' }: the same, as a line
 *   shortAccountLabel(a)       "<banco> Corriente", "<banco> Ahorros", "Tarjeta ···1111",
 *                              "IBKR U···567" (<banco>: the institution's short name)
 *   accountWithNumber(a)       "Corriente ···0932", "Ahorros ···4417", "Tarjeta ···1111",
 *                              "IBKR U···567": the account in a movement's meta line
 *   accountRowTitle(a)         the owner's alias, else "Ahorros", "Corriente",
 *                              "Tarjeta de crédito", "Interactive Brokers"
 *   accountRowSubtitle(a)      "<banco> Corriente ···0932" (when aliased), "<institución>
 *                              ···4417", "Inversiones · U···567"
 *   accountRowSubtitleParts(a) the same as { name, number }, so a row can truncate the
 *                              name and never the digits
 *   institutionInitials(i)     two or three letters ("IB"): the tile, never a bank logo
 *   groupAccounts(accounts, {always?})
 *                              [{ key, title, accounts, total }] for Cuentas, Tarjetas,
 *                              Inversiones and Tarjetas de otros, in that order, empty
 *                              groups left out unless `always` names them. Tarjetas de
 *                              otros has no total: what its cards were used for is
 *                              neither money the owner has nor money they owe
 *   hasShortcut(a)             whether the account has a working shortcut key
 *   shortcutNote(shortcut)     how its shortcut is doing: "Última compra hace 2 h",
 *                              "Probada, sin compras todavía", "Sin probar todavía"
 */

/** Builds an Amount from cents, for a sum computed in the browser. */
export function amountOf(cents: number): Amount {
  return { cents, formatted: (cents / 100).toFixed(2), currency: 'USD' }
}

/** What a card someone else pays is to the owner, said the same everywhere. */
export const PAID_BY_OTHER_NOTE = 'Cuenta en tus gastos, no en lo que debes.'

/**
 * Whether the account is a card someone else pays. Its purchases are the owner's
 * spending; its debt is not theirs, so it stays out of "Debes", the net worth and
 * every count of accounts that lack a starting balance or do not match.
 */
export function paidByOther(account: Pick<Account, 'paid_by_other'>): boolean {
  return account.paid_by_other === true
}

/**
 * Movements but no starting balance, so the balance is only their sum. A card
 * someone else pays never has one to lack: nothing printed reaches it.
 */
export function lacksOpening(
  account: Pick<Account, 'paid_by_other' | 'anchored' | 'movements'>,
): boolean {
  return !paidByOther(account) && !account.anchored && account.movements > 0
}

/** The one figure an account is summed up by. */
export function headline(account: Account): { amount: Amount; label: string } {
  // Before the liability branch: such a card is one, but what it owes is not the
  // owner's, and what they want to know is how much it was used this month.
  if (paidByOther(account)) {
    return { amount: account.this_month ?? amountOf(0), label: 'Este mes' }
  }
  if (account.class === 'liability') {
    return { amount: account.owed ?? amountOf(-account.balance.cents), label: 'Debes' }
  }
  if (account.type === 'brokerage') {
    return {
      amount: amountOf(account.balance.cents + (account.holdings?.cents ?? 0)),
      label: 'Total',
    }
  }
  return { amount: account.balance, label: 'Saldo' }
}

export type AccountStatus =
  | {
      kind: 'cuadra'
      /** The civil date of the bank figure it matches. */
      asOf: string
    }
  | {
      kind: 'no-cuadra'
      /** How far apart the bank and the movements are, always positive. */
      difference: Amount
      /** The civil date the bank's figure is from. */
      asOf: string
      /** The bank's figure as the bank prints it: a card's debt is positive. */
      stated: Amount
    }
  | { kind: 'sincronizado'; at: string }
  | { kind: 'sin-partida' }

/**
 * Whether an account agrees with its bank.
 *
 * - no-cuadra: the latest stated balance differs from what the movements add up to.
 *   It wins over everything else, a broker included.
 * - sincronizado: a brokerage account with a known last sync (needs `last_synced_at`).
 * - sin-partida: movements but no starting balance, so the balance is only a sum.
 *   Neutral, never orange.
 * - cuadra: the latest stated balance matches.
 * - null: nothing to say yet (no movements, or only an opening balance), and always
 *   for a card someone else pays: it has no balance of the owner's to check.
 */
export function accountStatus(account: Account): AccountStatus | null {
  if (paidByOther(account)) return null
  const drift = account.drift
  if (drift && drift.difference.cents !== 0) {
    const liability = account.class === 'liability'
    return {
      kind: 'no-cuadra',
      difference: amountOf(Math.abs(drift.difference.cents)),
      asOf: drift.as_of,
      stated: liability ? amountOf(-drift.stated.cents) : drift.stated,
    }
  }
  if (account.type === 'brokerage' && account.last_synced_at) {
    return { kind: 'sincronizado', at: account.last_synced_at }
  }
  if (lacksOpening(account)) return { kind: 'sin-partida' }
  if (!account.anchored) return null
  if (drift) return { kind: 'cuadra', asOf: drift.as_of }
  return null
}

/** Whether the account agrees with its bank, as one line of text. */
export function reconciliationNote(account: Account): {
  text: string
  tone: 'ok' | 'warn' | 'none'
} {
  if (paidByOther(account)) {
    return { text: PAID_BY_OTHER_NOTE, tone: 'none' }
  }
  if (!account.anchored && account.movements === 0) {
    return { text: 'Sin movimientos todavía', tone: 'none' }
  }
  const status = accountStatus(account)
  switch (status?.kind) {
    case 'no-cuadra':
      return {
        text: `El banco dice ${money(status.stated)} desde el ${formatCivilDate(status.asOf)}.`,
        tone: 'warn',
      }
    case 'sin-partida':
      return {
        text:
          account.class === 'liability'
            ? 'Sin saldo inicial: anota lo que debes según tu estado'
            : 'Sin saldo inicial',
        tone: 'warn',
      }
    case 'cuadra':
      return {
        text: `Cuadra con el banco al ${formatCivilDate(status.asOf)}`,
        tone: 'ok',
      }
    default:
      return { text: 'Saldo de partida del banco', tone: 'none' }
  }
}

const INSTITUTION_SHORT: Record<Institution, string> = {
  banco_general: 'BG',
  bac: 'BAC',
  ibkr: 'IBKR',
  // Only ever a card, which shortAccountLabel names by its number instead.
  manual: 'Otra persona',
}

const INSTITUTION_INITIALS: Record<Institution, string> = {
  banco_general: 'BG',
  bac: 'BAC',
  ibkr: 'IB',
  // Otra persona: who pays it, beside the owner's name for it.
  manual: 'OP',
}

const TYPE_SHORT: Record<Account['type'], string> = {
  checking: 'Corriente',
  savings: 'Ahorros',
  credit_card: 'Tarjeta',
  brokerage: 'IBKR',
}

const TYPE_TITLE: Record<Account['type'], string> = {
  checking: 'Corriente',
  savings: 'Ahorros',
  credit_card: 'Tarjeta de crédito',
  brokerage: 'Interactive Brokers',
}

/** Two or three letters ("IB" for Interactive Brokers): what an institution's tile shows. */
export function institutionInitials(institution: Institution | string): string {
  return (
    INSTITUTION_INITIALS[institution as Institution] ??
    institution.slice(0, 3).toUpperCase()
  )
}

function institutionShort(institution: Institution | string): string {
  return INSTITUTION_SHORT[institution as Institution] ?? institutionLabel(institution)
}

/**
 * The account in a title or a sentence: "<banco> Corriente no cuadra por $25.00".
 * Never the alias: this is how the bank would name it.
 */
export function shortAccountLabel(account: Account): string {
  const number = numberShort(account.external_number)
  // Named by what the owner calls it: "Tarjeta ···4321" could as well be one of theirs.
  if (paidByOther(account)) return account.alias?.trim() || `Tarjeta ${number}`
  if (account.type === 'credit_card') return `Tarjeta ${number}`
  if (account.type === 'brokerage')
    return `${institutionShort(account.institution)} ${number}`
  return `${institutionShort(account.institution)} ${TYPE_SHORT[account.type]}`
}

/** The account in a movement's meta line: "Ahorros ···4417", "Tarjeta ···1111". */
export function accountWithNumber(account: Account): string {
  return `${TYPE_SHORT[account.type] ?? account.type} ${numberShort(account.external_number)}`
}

/** An account row's first line: the owner's alias, or what kind of account it is. */
export function accountRowTitle(account: Account): string {
  return account.alias?.trim() || TYPE_TITLE[account.type] || account.display_name
}

/**
 * An account row's second line in two parts: `name` may be cut with an ellipsis,
 * `number` ("···1111", "U···567") never is, since it is what tells two accounts at
 * the same bank apart.
 */
export function accountRowSubtitleParts(account: Account): {
  name: string
  number: string
} {
  const number = numberShort(account.external_number)
  if (account.type === 'brokerage') return { name: 'Inversiones ·', number }
  if (account.alias?.trim() && account.type !== 'credit_card') {
    return { name: shortAccountLabel(account), number }
  }
  return { name: institutionLabel(account.institution), number }
}

/** An account row's second line, as one string. */
export function accountRowSubtitle(account: Account): string {
  const { name, number } = accountRowSubtitleParts(account)
  return `${name} ${number}`
}

/** A group of the owner's own accounts: what they have, owe or invest, with its total. */
interface OwnGroup {
  key: 'cuentas' | 'tarjetas' | 'inversiones'
  title: 'Cuentas' | 'Tarjetas' | 'Inversiones'
  accounts: Account[]
  /** The sum of each account's `headline` amount: balances, debt owed, or totals. */
  total: Amount
}

/**
 * The cards someone else pays. No total: what they were used for is spending, which
 * Gastos counts, and neither money the owner has nor money they owe. Leaving it out
 * keeps them out of every sum built from the groups (Patrimonio's "Debes").
 */
interface OthersCardsGroup {
  key: 'otras'
  title: 'Tarjetas de otros'
  accounts: Account[]
  total?: undefined
}

export type AccountGroup = OwnGroup | OthersCardsGroup

/**
 * Accounts in the groups the app shows: Cuentas, Tarjetas and Inversiones, each with
 * its total, then Tarjetas de otros. A group with no accounts is left out, unless
 * `always` names it (Cuentas keeps Inversiones and Tarjetas de otros, so connecting
 * IBKR and adding a card someone else pays each have a place to live).
 */
export function groupAccounts(
  accounts: Account[],
  { always = [] }: { always?: AccountGroup['key'][] } = {},
): AccountGroup[] {
  const own = (key: OwnGroup['key'], title: OwnGroup['title']): OwnGroup => ({
    key,
    title,
    accounts: [],
    total: amountOf(0),
  })
  const cuentas = own('cuentas', 'Cuentas')
  const tarjetas = own('tarjetas', 'Tarjetas')
  const inversiones = own('inversiones', 'Inversiones')
  const otras: OthersCardsGroup = { key: 'otras', title: 'Tarjetas de otros', accounts: [] }
  for (const account of accounts) {
    // First: such a card is a credit card too, but its debt is not the owner's.
    if (paidByOther(account)) {
      otras.accounts.push(account)
      continue
    }
    const group =
      account.type === 'credit_card'
        ? tarjetas
        : account.type === 'brokerage'
          ? inversiones
          : cuentas
    group.accounts.push(account)
    group.total = amountOf(group.total.cents + headline(account).amount.cents)
  }
  return [cuentas, tarjetas, inversiones, otras].filter(
    (group) => group.accounts.length > 0 || always.includes(group.key),
  )
}

// --- the phone's shortcut ---------------------------------------------------

/** Whether the account has a shortcut key that works. */
export function hasShortcut(account: Account): boolean {
  return account.shortcut?.configured === true
}

/**
 * How an account's shortcut is doing, from its key: the last purchase it noted, or
 * whether it was ever run by hand (which tests it and notes nothing).
 */
export function shortcutNote(shortcut: ShortcutStatus): string {
  if (shortcut.last_capture_at)
    return `Última compra ${relativeTime(shortcut.last_capture_at)}`
  if (shortcut.tested_at) return 'Probada, sin compras todavía'
  return 'Sin probar todavía'
}
