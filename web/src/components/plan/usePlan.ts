/**
 * The month's plan, read and written.
 *
 *   planKey(month?)        ['reports', 'plan', month ?? 'current']
 *   usePlan(month?)        GET /api/plan. The current month is asked for without a
 *                          month, so the plan page, Gastos' card and Inicio share one
 *                          entry, and the server (Panama time) says which month that is
 *   useSavePlanItem()({id, input}) · useDeletePlanItem()({id, month, scope})
 *   useSavePlanCap()({categoryId, input}) · useDeletePlanCap()({categoryId, month, scope})
 *   usePlanLinks(about, onDone, onFailure)
 *                          a sheet's answers about one month of an item (which
 *                          movement paid it, or that it was paid outside), and taking
 *                          one back, each with its toast and "Deshacer"
 *   usePlanUndo()(step)    runs an undo from a toast and refreshes the plan; says so
 *                          when it fails
 *
 * The key sits under 'reports', one of lib/queries' money prefixes, so anything that
 * changes money (an import, a category, a transfer confirmed) refreshes the plan with
 * no extra wiring. A plan edit changes no money: it refreshes only the plan, never
 * the rest of the app. Defined here rather than in lib/queries, whose hooks are
 * frozen; the endpoints are api/endpoints' plan functions.
 *
 * Every write refreshes the plan once it settles, failed or not: a send that got no
 * answer may still have been saved, and the plan on screen must not lag behind it.
 */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import {
  addPlanLink,
  deletePlanCap,
  deletePlanItem,
  deletePlanLink,
  fetchPlan,
  savePlanCap,
  savePlanItem,
  type PlanCapInput,
  type PlanItemInput,
  type PlanLinkSaying,
} from '@/api/endpoints'
import type { PlanPaidMovement, PlanScope } from '@/api/types'
import { problemMessage } from '@/components/settings/problem'

const PLAN_PREFIX = ['reports', 'plan'] as const

export const planKey = (month?: string) => [...PLAN_PREFIX, month ?? 'current'] as const

export function usePlan(month?: string) {
  return useQuery({ queryKey: planKey(month), queryFn: () => fetchPlan(month) })
}

/** Refreshes every month's plan, and nothing else. */
export function usePlanInvalidation() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: PLAN_PREFIX })
}

export function useSavePlanItem() {
  const invalidate = usePlanInvalidation()
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: PlanItemInput }) =>
      savePlanItem(id, input),
    onSettled: invalidate,
  })
}

export function useDeletePlanItem() {
  const invalidate = usePlanInvalidation()
  return useMutation({
    mutationFn: ({ id, month, scope }: { id: string; month: string; scope: PlanScope }) =>
      deletePlanItem(id, month, scope),
    onSettled: invalidate,
  })
}

export function useSavePlanCap() {
  const invalidate = usePlanInvalidation()
  return useMutation({
    mutationFn: ({ categoryId, input }: { categoryId: string; input: PlanCapInput }) =>
      savePlanCap(categoryId, input),
    onSettled: invalidate,
  })
}

export function useDeletePlanCap() {
  const invalidate = usePlanInvalidation()
  return useMutation({
    mutationFn: ({
      categoryId,
      month,
      scope,
    }: {
      categoryId: string
      month: string
      scope: PlanScope
    }) => deletePlanCap(categoryId, month, scope),
    onSettled: invalidate,
  })
}

/**
 * Takes a link back. One the server no longer has (plan_link_not_found: taken back
 * already, from another tab or by a "Deshacer" whose answer was lost) is how the
 * owner asked it to be, so it counts as done.
 */
function takeBackLink(id: string): Promise<void> {
  return deletePlanLink(id).catch((error: unknown) => {
    if (!(error instanceof ApiError && error.code === 'plan_link_not_found')) throw error
  })
}

/**
 * What a sheet says about one month of a paycheck or a payment (`about`). Each
 * answer calls `onDone` (the sheet closes) with a toast whose "Deshacer" takes it
 * back; a refusal goes to `onFailure`, for the sheet to show, which hears null when
 * an answer starts.
 *
 *   assign(movementId, words)  this movement paid it, or brought it in
 *   markPaid(words)            it was paid, or came in, where chenchén cannot see
 *   unmarkPaid(linkId, words)  takes that back, by the link that said it
 *   notThis(movement, words)   "No es este": the movement did not pay it
 *   busy                       an answer is on its way; nothing else is sent
 *
 * "No es este" always says so (`reject`), even about a movement the owner picked
 * themselves: taking that pick back would only let the plan look again, and its text
 * may well recognise the same movement, which would then stay. The server replaces
 * the pick with the new word. Its "Deshacer" deletes that word for a movement the
 * plan had picked, and picks the movement again for one the owner had.
 *
 * Saying sends a link with a new id. The server may answer with the one it had
 * already (the same word about the same movement and month), so "Deshacer" deletes
 * the link the answer names, not the id sent. Taking one back deletes it, and
 * "Deshacer" says it again with a new id. A new id per answer is safe: the server
 * keeps one link per word about a movement and month, and one "paid" per month of an
 * item, whatever ids they arrive with.
 */
export function usePlanLinks(
  about: { item_id: string; month: string },
  onDone: () => void,
  onFailure: (message: string | null) => void,
) {
  const invalidate = usePlanInvalidation()
  const add = useMutation({ mutationFn: addPlanLink, onSettled: invalidate })
  const remove = useMutation({ mutationFn: takeBackLink, onSettled: invalidate })
  const undo = usePlanUndo()
  const busy = add.isPending || remove.isPending
  const refused = (error: unknown) => onFailure(problemMessage(error))

  /** Says it again, with a new id: the undo of taking something back. */
  const sayAgain = (saying: PlanLinkSaying) => () =>
    addPlanLink({ ...saying, id: crypto.randomUUID() })

  /** Says `saying`; its "Deshacer" is `putBack`, or deleting the link it made. */
  const say = (saying: PlanLinkSaying, words: string, putBack?: () => Promise<unknown>) => {
    if (busy) return
    onFailure(null)
    add.mutate(
      { ...saying, id: crypto.randomUUID() },
      {
        onSuccess: (link) => {
          const step = putBack ?? (() => takeBackLink(link.id))
          toast(words, { action: { label: 'Deshacer', onClick: () => undo(step) } })
          onDone()
        },
        onError: refused,
      },
    )
  }

  const takeBack = (id: string, saying: PlanLinkSaying, words: string) => {
    if (busy) return
    onFailure(null)
    remove.mutate(id, {
      onSuccess: () => {
        toast(words, {
          action: { label: 'Deshacer', onClick: () => undo(sayAgain(saying)) },
        })
        onDone()
      },
      onError: refused,
    })
  }

  return {
    busy,
    assign: (movementId: string, words: string) =>
      say({ ...about, decision: 'assign', movement_id: movementId }, words),
    markPaid: (words: string) => say({ ...about, decision: 'paid' }, words),
    unmarkPaid: (linkId: string, words: string) =>
      takeBack(linkId, { ...about, decision: 'paid' }, words),
    notThis: (movement: PlanPaidMovement, words: string) =>
      say(
        { ...about, decision: 'reject', movement_id: movement.id },
        words,
        movement.how === 'assigned'
          ? sayAgain({ ...about, decision: 'assign', movement_id: movement.id })
          : undefined,
      ),
  }
}

/**
 * Undo from a toast. The dialog or sheet that made the change is gone by then, so
 * the step goes straight to the server; the plan refreshes after it either way.
 */
export function usePlanUndo() {
  const invalidate = usePlanInvalidation()
  return (step: () => Promise<unknown>) => {
    step().then(
      () => invalidate(),
      () => {
        void invalidate()
        toast('No se pudo deshacer. Intenta otra vez.')
      },
    )
  }
}
