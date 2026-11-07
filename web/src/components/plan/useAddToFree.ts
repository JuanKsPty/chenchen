/**
 * useAddToFree(plan)  { amount, paycheck, addable, run, busy }: what the hero says
 * about income that came in and no paycheck of the plan took, and "Sumar a lo
 * libre".
 *
 *   amount    what came in unplanned this month, while the month can be edited; 0
 *             when the hero says nothing about it
 *   paycheck  a paycheck of the month that money may well be: it has not come in,
 *             and either it has no text to be recognised by or its day has come,
 *             and its amount is close to what came in. The plan only recognises a
 *             paycheck by its text, so one saved without it reads as money outside
 *             the plan when it arrives. Then the hero says "que no reconocimos" and
 *             leads to the paycheck, never to "Sumar a lo libre": that would count
 *             the paycheck twice.
 *   addable   whether "Sumar a lo libre" would count exactly that money, once: it is
 *             offered only then
 *
 * Income that came in unplanned (a bonus, a refund of a deposit) never changes what
 * is free by itself: the plan counts what was expected, so a windfall cannot be
 * spent twice in the owner's head. "Sumar a lo libre" is the one tap that says
 * "count it": a paycheck item for this month alone ("Entrada extra"), for what came
 * in unplanned, so it holds for no other month.
 *
 * The item has to take those movements, or they would still read as unplanned and
 * the button would stay, adding them again with every tap: each is linked to it.
 * Which movements they are comes from the month's income less the ones a paycheck of
 * this month took; it is trusted only when they add up to exactly what the server
 * says came in unplanned (otherwise one may belong to a paycheck of the month next
 * door). Otherwise the button is not offered: the line stays, without it.
 *
 * One id per attempt, and one per link, kept until it succeeds, so a retry never
 * makes two of anything. A link the server refuses takes the item back out before
 * saying so, so what is free never grows without its "Deshacer". The toast's
 * "Deshacer" takes the item out of this month, which removes it and its links.
 */
import { useQuery } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { toast } from 'sonner'

import {
  addPlanLink,
  deletePlanItem,
  fetchEntries,
  savePlanItem,
  type EntryQuery,
} from '@/api/endpoints'
import type { Entry, EntryPage, MonthPlan, PlanIncomeLine } from '@/api/types'
import { problemMessage } from '@/components/settings/problem'
import { money, monthBounds } from '@/lib/format'
import { keys } from '@/lib/queries'
import { centsText } from './forms'
import { usePlanInvalidation, usePlanUndo } from './usePlan'
import { monthWord } from './words'

/** How many of the month's income movements are read; more, and none is trusted. */
const INCOME_PAGE = 200

/** One try at "Sumar a lo libre": the item's id and each movement's link id. */
interface Attempt {
  id: string
  links: Map<string, string>
}

export interface AddToFree {
  amount: number
  paycheck: PlanIncomeLine | null
  addable: boolean
  run: () => Promise<void>
  busy: boolean
}

export function useAddToFree(plan: MonthPlan): AddToFree {
  const invalidate = usePlanInvalidation()
  const undo = usePlanUndo()
  const [busy, setBusy] = useState(false)
  const attempt = useRef<Attempt | null>(null)

  const amount =
    plan.state === 'current' && plan.editable ? Math.max(0, plan.income.unplanned.cents) : 0
  const query = incomeQuery(plan.month)
  const entries = useQuery({
    queryKey: keys.entries(query),
    queryFn: () => fetchEntries(query),
    enabled: amount > 0,
  })
  const movements = amount > 0 && entries.data ? unplannedIncome(plan, entries.data) : null
  const paycheck = amount > 0 ? mayBe(plan, amount, movements) : null
  const addable = !paycheck && movements !== null && movements.length > 0

  const run = async () => {
    if (busy || !addable) return
    setBusy(true)
    const month = plan.month
    attempt.current ??= { id: crypto.randomUUID(), links: new Map() }
    const current = attempt.current
    const { id } = current
    const first = movements[0]

    const finish = (failure?: unknown) => {
      if (failure !== undefined) toast.error(problemMessage(failure))
      void invalidate()
      setBusy(false)
    }

    try {
      await savePlanItem(id, {
        kind: 'income',
        month,
        scope: 'only',
        name: 'Entrada extra',
        amount: centsText(amount),
        day: Number((first?.booked_on ?? plan.today).slice(8, 10)),
      })
    } catch (error) {
      // Saved or not, the next try sends the same id, so it is never made twice.
      return finish(error)
    }

    try {
      for (const movement of movements) {
        let linkId = current.links.get(movement.id)
        if (!linkId) {
          linkId = crypto.randomUUID()
          current.links.set(movement.id, linkId)
        }
        await addPlanLink({
          id: linkId,
          item_id: id,
          month,
          movement_id: movement.id,
          decision: 'assign',
        })
      }
    } catch (error) {
      // Half done, the item would add to what is free money the plan still calls
      // unplanned, with no "Deshacer": it goes, and its links with it.
      try {
        await deletePlanItem(id, month, 'only')
        attempt.current = null
      } catch {
        // Still there: the next try finishes it with the same ids.
      }
      return finish(error)
    }

    attempt.current = null
    toast(`Listo: sumamos ${money(amount)} a lo libre de ${monthWord(month)}.`, {
      action: {
        label: 'Deshacer',
        onClick: () => undo(() => deletePlanItem(id, month, 'only')),
      },
    })
    finish()
  }

  return { amount, paycheck, addable, run, busy }
}

/** The month's income movements, as one page. */
function incomeQuery(month: string): EntryQuery {
  const { from, to } = monthBounds(month)
  return { kinds: ['income'], from, to, limit: INCOME_PAGE }
}

/**
 * The month's income movements no paycheck of this month took, when they add up to
 * exactly what the plan says came in unplanned; null when they do not, or when the
 * month has more income than one page holds.
 */
function unplannedIncome(plan: MonthPlan, page: EntryPage): Entry[] | null {
  if (page.has_more) return null
  const taken = new Set(plan.income.items.flatMap((i) => i.movements.map((m) => m.id)))
  const left = page.entries.filter((e) => e.amount.cents > 0 && !taken.has(e.id))
  const sum = left.reduce((acc, e) => acc + e.amount.cents, 0)
  return sum === plan.income.unplanned.cents ? left : null
}

/**
 * The first paycheck of the month that the unplanned money may be (see the module
 * comment): close to all of it, or to one of its movements when they are known.
 */
function mayBe(
  plan: MonthPlan,
  amount: number,
  movements: Entry[] | null,
): PlanIncomeLine | null {
  const sizes = [amount, ...(movements ?? []).map((m) => m.amount.cents)]
  return (
    plan.income.items.find(
      (line) =>
        line.status !== 'done' &&
        (!line.match_text.trim() || line.due_on <= plan.today) &&
        sizes.some((cents) => near(line.amount.cents, cents)),
    ) ?? null
  )
}

/**
 * Whether `cents` could be a paycheck of `planned` by the server's own measure: a
 * quarter either way, a dollar at least.
 */
function near(planned: number, cents: number): boolean {
  return Math.abs(cents - planned) <= Math.max(100, Math.trunc((planned * 25) / 100))
}
