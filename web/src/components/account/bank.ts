/**
 * What an account page says about the bank's own figures.
 *
 *   accountSubtitle(a)         "Gastos del mes · ···0932", "<institución> · ···4417",
 *                              "<institución>" (a card's title already has its number)
 *   SOURCE_LABEL               where a stated balance came from, in words
 *   SOURCE_ICON                its tile icon
 *   shortDate(ymd)             "03 oct" this year, "30 dic 2025" otherwise
 *   statedAmount(account, c)   a stated balance as the bank prints it (a card's debt
 *                              is positive)
 *   bankFigures(checkpoints)   newest first, one row per day and figure: a statement's
 *                              opening repeats the previous closing, so it is dropped
 *   cardFacts(account, cps)    { closingOn, dueOn, minimum } from the newest checkpoint
 *                              with a due date, else from `account.statement`
 *   differenceById(checks)     checkpoint id → stated − computed, from the reconciliation
 *   LOAD_FAILED                spec voice errorGeneric, for a part of the page that
 *                              could not load (never shown as "nothing here yet")
 */
import { FileTextIcon, ScaleIcon, TrendingUpIcon, type LucideIcon } from 'lucide-react'

import type { Account, Amount, Checkpoint, ReconciliationCheck } from '@/api/types'
import { amountOf } from '@/lib/account'
import {
  formatCivilDate,
  formatCivilDayMonth,
  institutionLabel,
  numberShort,
  todayCivil,
} from '@/lib/format'

/** The line under an account's title: its alias or bank, and the number unless the title has it. */
export function accountSubtitle(account: Account): string {
  const first = account.alias?.trim() || institutionLabel(account.institution)
  const titled = account.type === 'credit_card' || account.type === 'brokerage'
  return titled ? first : `${first} · ${numberShort(account.external_number)}`
}

export const LOAD_FAILED =
  'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'

export const SOURCE_LABEL: Record<Checkpoint['source'], string> = {
  statement_closing: 'Cierre del estado',
  statement_opening: 'Inicio del estado',
  broker: 'Reporte de IBKR',
  manual: 'Lo anotaste tú',
}

export const SOURCE_ICON: Record<Checkpoint['source'], LucideIcon> = {
  statement_closing: FileTextIcon,
  statement_opening: FileTextIcon,
  broker: TrendingUpIcon,
  manual: ScaleIcon,
}

/** "03 oct" in the current year, "30 dic 2025" in any other. */
export function shortDate(ymd: string): string {
  return ymd.slice(0, 4) === todayCivil().slice(0, 4)
    ? formatCivilDayMonth(ymd)
    : formatCivilDate(ymd)
}

/** A stated balance as the bank prints it: what a card owes reads positive. */
export function statedAmount(account: Account, balance: Amount): Amount {
  return account.class === 'liability' ? amountOf(-balance.cents) : balance
}

const SOURCE_RANK: Record<Checkpoint['source'], number> = {
  manual: 1,
  broker: 2,
  statement_closing: 3,
  statement_opening: 4,
}

function rank(checkpoint: Checkpoint): number {
  return checkpoint.pinned ? 0 : SOURCE_RANK[checkpoint.source]
}

/**
 * The bank's figures, newest first. A statement's opening balance is the previous
 * statement's closing on the same day, so only one of a day's identical figures is
 * kept: the starting balance if one of them is it, else the one the owner typed,
 * else the closing.
 */
export function bankFigures(checkpoints: Checkpoint[] | undefined): Checkpoint[] {
  const kept = new Map<string, Checkpoint>()
  for (const checkpoint of checkpoints ?? []) {
    const key = `${checkpoint.as_of}|${checkpoint.balance.cents}`
    const previous = kept.get(key)
    if (!previous || rank(checkpoint) < rank(previous)) kept.set(key, checkpoint)
  }
  return [...kept.values()].sort((a, b) =>
    a.as_of === b.as_of ? rank(a) - rank(b) : a.as_of < b.as_of ? 1 : -1,
  )
}

export interface CardFacts {
  closingOn: string
  dueOn: string | null
  minimum: Amount | null
}

/** A card's latest statement: the cut-off day, when to pay and the minimum. */
export function cardFacts(
  account: Account,
  checkpoints: Checkpoint[] | undefined,
): CardFacts | null {
  if (account.type !== 'credit_card') return null
  const latest = [...(checkpoints ?? [])]
    .filter((checkpoint) => checkpoint.due_on)
    .sort((a, b) => (a.as_of < b.as_of ? 1 : a.as_of > b.as_of ? -1 : 0))[0]
  if (latest) {
    return {
      closingOn: latest.as_of,
      dueOn: latest.due_on ?? null,
      minimum: latest.minimum_payment ?? null,
    }
  }
  if (account.statement) {
    return {
      closingOn: account.statement.closing_on,
      dueOn: account.statement.due_on,
      minimum: account.statement.minimum_payment,
    }
  }
  return null
}

/** Each stated balance's difference with the movements (stated − computed). */
export function differenceById(
  checks: ReconciliationCheck[] | undefined,
): Map<string, Amount> {
  return new Map((checks ?? []).map((check) => [check.checkpoint.id, check.difference]))
}
