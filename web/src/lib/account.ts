import type { Account, Amount, Institution } from '@/api/types'
import { formatCivilDate, institutionLabel, money, numberShort } from '@/lib/format'

/**
 * What the app says about an account, in one place.
 *
 *   amountOf(cents)            an Amount built from cents (a sum made in the browser)
 *   headline(a)                { amount, label }: the one figure an account is summed
 *                              up by. Debes (owed) for a card, Total (cash plus
 *                              positions) for a broker, Saldo otherwise.
 *   accountStatus(a)           { kind: 'cuadra', asOf }
 *                              | { kind: 'no-cuadra', difference, asOf, stated }
 *                              | { kind: 'sincronizado', at }
 *                              | { kind: 'sin-partida' }
 *                              | null (nothing worth a chip)
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
 *   groupAccounts(accounts)    [{ key, title, accounts, total }] for Cuentas, Tarjetas
 *                              and Inversiones, in that order, empty groups left out
 */

/** Builds an Amount from cents, for a sum computed in the browser. */
export function amountOf(cents: number): Amount {
  return { cents, formatted: (cents / 100).toFixed(2), currency: 'USD' }
}

/** The one figure an account is summed up by. */
export function headline(account: Account): { amount: Amount; label: string } {
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
 * - null: nothing to say yet (no movements, or only an opening balance).
 */
export function accountStatus(account: Account): AccountStatus | null {
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
  if (!account.anchored) {
    return account.movements > 0 ? { kind: 'sin-partida' } : null
  }
  if (drift) return { kind: 'cuadra', asOf: drift.as_of }
  return null
}

/** Whether the account agrees with its bank, as one line of text. */
export function reconciliationNote(account: Account): {
  text: string
  tone: 'ok' | 'warn' | 'none'
} {
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
}

const INSTITUTION_INITIALS: Record<Institution, string> = {
  banco_general: 'BG',
  bac: 'BAC',
  ibkr: 'IB',
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

export interface AccountGroup {
  key: 'cuentas' | 'tarjetas' | 'inversiones'
  title: 'Cuentas' | 'Tarjetas' | 'Inversiones'
  accounts: Account[]
  /** The sum of each account's `headline` amount: balances, debt owed, or totals. */
  total: Amount
}

/** Accounts in the three groups the app shows, each with its total. */
export function groupAccounts(accounts: Account[]): AccountGroup[] {
  const cuentas: AccountGroup = {
    key: 'cuentas',
    title: 'Cuentas',
    accounts: [],
    total: amountOf(0),
  }
  const tarjetas: AccountGroup = {
    key: 'tarjetas',
    title: 'Tarjetas',
    accounts: [],
    total: amountOf(0),
  }
  const inversiones: AccountGroup = {
    key: 'inversiones',
    title: 'Inversiones',
    accounts: [],
    total: amountOf(0),
  }
  for (const account of accounts) {
    const group =
      account.type === 'credit_card'
        ? tarjetas
        : account.type === 'brokerage'
          ? inversiones
          : cuentas
    group.accounts.push(account)
    group.total = amountOf(group.total.cents + headline(account).amount.cents)
  }
  return [cuentas, tarjetas, inversiones].filter((group) => group.accounts.length > 0)
}
