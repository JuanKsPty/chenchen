import {
  ArrowLeftRightIcon,
  ChartPieIcon,
  HouseIcon,
  WalletIcon,
  type LucideIcon,
} from 'lucide-react'

import type { EntryQuery } from '@/api/endpoints'
import type { EntryKind } from '@/api/types'

/**
 * Where things live. The sections, every path, and the URL formats other screens
 * link into. These formats are frozen: a link written by one screen has to keep
 * opening the right view in another.
 *
 *   NAV                    the four sections, in order, for the desktop tabs and the
 *                          mobile tab bar: Inicio, Cuentas, Movimientos, Gastos
 *   HOME_PATH ... SETTINGS_PATH   every signed-in path
 *
 *   movementsHref(filters?)        /movimientos?q=&cuenta=&categoria=&sin_categoria=1
 *                                  &desde=YYYY-MM-DD&hasta=YYYY-MM-DD&tipo=<EntryKind>
 *   parseMovementFilters(params)   the same, read back from the URL (validated)
 *   movementFiltersToParams(f)     the same, as URLSearchParams (for setSearchParams)
 *   movementFiltersToQuery(f)      the same, as the API's EntryQuery
 *
 *   accountHref(id, {review?})     /cuentas/:id, plus ?revisar=1 to open the review
 *   isReviewRequested(params)      true when ?revisar=1 is present
 *
 *   spendingHref(ym?)              /gastos, or /gastos?mes=YYYY-MM
 *   parseSpendingMonth(params)     the ?mes value when it is a valid YYYY-MM
 *   TRANSFERS_HREF                 /gastos#transferencias
 */

export const HOME_PATH = '/inicio'
export const ACCOUNTS_PATH = '/cuentas'
export const MOVEMENTS_PATH = '/movimientos'
export const SPENDING_PATH = '/gastos'
export const RULES_PATH = '/gastos/reglas'
export const SETTINGS_PATH = '/ajustes'
/** Ajustes, scrolled to the PIN and device section. */
export const SECURITY_HREF = `${SETTINGS_PATH}#seguridad`
/** Gastos, scrolled to the transfers waiting for a yes or no. */
export const TRANSFERS_HREF = `${SPENDING_PATH}#transferencias`

export interface NavItem {
  to: string
  label: string
  icon: LucideIcon
}

/** The four sections. Same names everywhere: tabs, tab bar, titles. */
export const NAV: NavItem[] = [
  { to: HOME_PATH, label: 'Inicio', icon: HouseIcon },
  { to: ACCOUNTS_PATH, label: 'Cuentas', icon: WalletIcon },
  { to: MOVEMENTS_PATH, label: 'Movimientos', icon: ArrowLeftRightIcon },
  { to: SPENDING_PATH, label: 'Gastos', icon: ChartPieIcon },
]

// --- movements ---------------------------------------------------------------

/** What Movimientos is filtered by. Every field is optional; none means everything. */
export interface MovementFilters {
  /** Free text. URL `q`. */
  search?: string
  /** An account id. URL `cuenta`. */
  account?: string
  /** A category id. URL `categoria`. Ignored when `uncategorized` is set. */
  category?: string
  /** Only movements with no category. URL `sin_categoria=1`. */
  uncategorized?: boolean
  /** First day, inclusive, YYYY-MM-DD. URL `desde`. */
  from?: string
  /** Last day, inclusive, YYYY-MM-DD. URL `hasta`. */
  to?: string
  /** One entry kind. URL `tipo`. */
  kind?: EntryKind
}

const ENTRY_KINDS: readonly EntryKind[] = [
  'income',
  'expense',
  'refund',
  'fee',
  'interest',
  'transfer',
  'trade',
]

const CIVIL_DATE = /^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$/
const YEAR_MONTH = /^\d{4}-(0[1-9]|1[0-2])$/

/**
 * A day that exists. The pattern allows the 31st in any month; the round trip
 * through Date rolls 2026-02-31 over into March, so comparing the parts back drops
 * it, rather than letting the chip show 03 mar and the API refuse the list.
 */
function isCivilDate(value: string | null): value is string {
  if (!value || !CIVIL_DATE.test(value)) return false
  const [year = 0, month = 0, day = 0] = value.split('-').map(Number)
  const date = new Date(Date.UTC(year, month - 1, day))
  return date.getUTCMonth() === month - 1 && date.getUTCDate() === day
}

export function movementFiltersToParams(filters: MovementFilters = {}): URLSearchParams {
  const params = new URLSearchParams()
  const search = filters.search?.trim()
  if (search) params.set('q', search)
  if (filters.account) params.set('cuenta', filters.account)
  if (filters.category && !filters.uncategorized) params.set('categoria', filters.category)
  if (filters.uncategorized) params.set('sin_categoria', '1')
  if (filters.from) params.set('desde', filters.from)
  if (filters.to) params.set('hasta', filters.to)
  if (filters.kind) params.set('tipo', filters.kind)
  return params
}

/** A link to Movimientos, already filtered. */
export function movementsHref(filters: MovementFilters = {}): string {
  const query = movementFiltersToParams(filters).toString()
  return query ? `${MOVEMENTS_PATH}?${query}` : MOVEMENTS_PATH
}

/** Reads the filters back from Movimientos' URL, dropping anything malformed. */
export function parseMovementFilters(params: URLSearchParams): MovementFilters {
  const filters: MovementFilters = {}
  const search = params.get('q')?.trim()
  if (search) filters.search = search
  const account = params.get('cuenta')?.trim()
  if (account) filters.account = account
  if (params.get('sin_categoria') === '1') filters.uncategorized = true
  const category = params.get('categoria')?.trim()
  if (category && !filters.uncategorized) filters.category = category
  const from = params.get('desde')
  if (isCivilDate(from)) filters.from = from
  const to = params.get('hasta')
  if (isCivilDate(to)) filters.to = to
  const kind = params.get('tipo') as EntryKind | null
  if (kind && ENTRY_KINDS.includes(kind)) filters.kind = kind
  return filters
}

/** The filters as the API's movement query (no cursor or limit). */
export function movementFiltersToQuery(
  filters: MovementFilters,
): Omit<EntryQuery, 'cursor' | 'limit'> {
  return {
    search: filters.search?.trim() || undefined,
    accountId: filters.account,
    categoryId: filters.uncategorized ? undefined : filters.category,
    uncategorized: filters.uncategorized || undefined,
    from: filters.from,
    to: filters.to,
    kinds: filters.kind ? [filters.kind] : undefined,
  }
}

// --- accounts ----------------------------------------------------------------

/** The account's page; with `review` it opens the "why it doesn't match" sheet. */
export function accountHref(id: string, options: { review?: boolean } = {}): string {
  const path = `${ACCOUNTS_PATH}/${encodeURIComponent(id)}`
  return options.review ? `${path}?revisar=1` : path
}

export function isReviewRequested(params: URLSearchParams): boolean {
  return params.get('revisar') === '1'
}

// --- spending ------------------------------------------------------------------

/** Gastos for a month (YYYY-MM); without one, the current month. */
export function spendingHref(ym?: string): string {
  return ym ? `${SPENDING_PATH}?mes=${ym}` : SPENDING_PATH
}

/** The month Gastos was asked for, when `?mes` is a valid YYYY-MM. */
export function parseSpendingMonth(params: URLSearchParams): string | undefined {
  const month = params.get('mes')
  return month && YEAR_MONTH.test(month) ? month : undefined
}
