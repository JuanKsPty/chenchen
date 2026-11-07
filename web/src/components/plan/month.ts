/**
 * planMonth(requested?)  { month, current, last } for the plan's ?mes.
 *
 * Any past month, this one, or next month, which can already be planned (a plan
 * carries forward, so it is this month's until something changes). A missing or
 * malformed month, or one further ahead, is this month. `last` is next month: how
 * far the month picker steps. The picker names months by the browser's calendar,
 * as Gastos does; which month is current for the figures is the server's call.
 */
import { currentMonth, shiftMonth } from '@/lib/format'

export function planMonth(requested?: string): {
  month: string
  current: boolean
  last: string
} {
  const now = currentMonth()
  const last = shiftMonth(now, 1)
  const month = requested && requested <= last ? requested : now
  return { month, current: month === now, last }
}
