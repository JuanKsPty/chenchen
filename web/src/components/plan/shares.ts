/**
 * What a planned purchase sets aside, worked out the way the server does (plan.Shares)
 * so a dialog can say it before anything is saved. The server writes the shares; this
 * only says what it will write, in integer cents, never a float.
 *
 *   monthsThrough(from, to)          how many months from `from` to `to` (YYYY-MM),
 *                                    both counted: 1 for the same month
 *   purchaseShares(left, from, to)   what each month from `from` to `to` sets aside of
 *                                    `left`, the part of the price no earlier month set
 *                                    aside: the k-th of n months takes
 *                                    ⌊left·(k+1)/n⌋ − ⌊left·k/n⌋, so the shares differ
 *                                    by a cent at most and add up to `left` exactly
 *   leftToSetAside(price, line)      `left` for a save from the month `line` was read
 *                                    for: the price, less what the months before it
 *                                    set aside, which an edit never changes
 *   setAsideSoFar(line)              what the plan has set aside for it up to the
 *                                    month read, that month included
 */
import type { PlanPurchaseLine } from '@/api/types'

/** ⌊a / n⌋ for a ≥ 0 and n > 0, on integers only. */
function floorDiv(a: number, n: number): number {
  return (a - (a % n)) / n
}

export function monthsThrough(from: string, to: string): number {
  const [fy = 0, fm = 1] = from.split('-').map(Number)
  const [ty = 0, tm = 1] = to.split('-').map(Number)
  return (ty - fy) * 12 + (tm - fm) + 1
}

export function purchaseShares(left: number, from: string, to: string): number[] {
  const n = monthsThrough(from, to)
  if (left <= 0 || n <= 0) return []
  return Array.from(
    { length: n },
    (_, k) => floorDiv(left * (k + 1), n) - floorDiv(left * k, n),
  )
}

/**
 * The price `cents` less what the months before `month` set aside, for an edit of
 * `line` (null for a new purchase, which nothing was set aside for yet). The view's
 * `set_aside_before` is exactly that sum for the month it was read for.
 */
export function leftToSetAside(cents: number, line: PlanPurchaseLine | null): number {
  return cents - (line?.set_aside_before.cents ?? 0)
}

export function setAsideSoFar(line: PlanPurchaseLine): number {
  return line.set_aside_before.cents + line.share.cents
}
