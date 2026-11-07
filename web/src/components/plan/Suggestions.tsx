/**
 * What the owner's history suggests planning, once the plan exists.
 *
 *   <SuggestionList plan title suggestions fallback? />
 *     "¿También son fijos?" under the payments, "¿También es un sueldo?" under the
 *     paychecks, "¿También es ahorro?" under the savings: the month's suggestions of
 *     one kind, each with "Agregar" and "No". Three at most until "Ver más".
 *   suggestedItem(s, month)  the item a suggestion fills in, from `month` on
 *   saveSuggested(id, input) saves it, leaving out a category the item cannot have
 *
 * Shown only for the current month while it can be edited (the page decides): what
 * the history suggests is about the months that came before it.
 *
 * "Agregar" plans it from this month on, as the owner's payment dialog would: its
 * name, its latest amount, its usual day and the text its statement prints, so the
 * plan recognises the next one. "No" turns it down, so it is not suggested again.
 * Neither is a primary (the view keeps one): a secondary and a ghost. Each answer
 * takes the row away at once and says so in a toast whose "Deshacer" takes the item
 * out, or the "No" back, and brings the row back; a refusal brings it back too, with
 * the server's sentence. Focus moves to the next row's "Agregar", or to `fallback`
 * (the card's own action) when none is left. A retry of "Agregar" sends the same
 * item id, so an answer that was lost never plans it twice.
 *
 * Each answer refreshes the plan and the suggestions, which then no longer hold what
 * was answered: the server leaves out what the plan has and what was turned down.
 * The row stays away until that list arrives, and no longer: the server decides what
 * is left, so one that comes back later (its item taken out of the plan) is offered
 * again.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useId, useRef, useState, type RefObject } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import {
  deletePlanItem,
  dismissPlanSuggestion,
  savePlanItem,
  undismissPlanSuggestion,
  type PlanItemInput,
} from '@/api/endpoints'
import type { MonthPlan, PlanSuggestion } from '@/api/types'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { unlessGone } from './forms'
import { planSuggestionsKey, usePlanInvalidation, usePlanUndo } from './usePlan'
import { suggestionHint } from './words'

/** How many suggestions a list shows before "Ver más". */
const SHOWN = 3

/**
 * The item a suggestion fills in, from `month` on: a paycheck or a payment with its
 * day, the text its statement prints and the category its lines are filed under; a
 * saving with its account, which is how the plan recognises it instead.
 */
export function suggestedItem(s: PlanSuggestion, month: string): PlanItemInput {
  const base = { month, scope: 'from' as const, name: s.name, amount: s.amount.formatted }
  if (s.kind === 'saving') {
    return {
      ...base,
      kind: 'saving',
      account_id: s.account_id ?? null,
      match_text: '',
      category_id: null,
    }
  }
  return {
    ...base,
    kind: s.kind,
    day: s.day,
    match_text: s.match_text,
    category_id: s.category_id ?? null,
  }
}

/**
 * Saves an item a suggestion filled in. Its category is the one its lines are filed
 * under most often, which the owner never chose here: one the item cannot have (an
 * income category on lines that go out, say) is left out rather than refusing the
 * whole item, so the plan never stops on a field the owner was not shown.
 */
export async function saveSuggested(id: string, input: PlanItemInput) {
  try {
    return await savePlanItem(id, input)
  } catch (error) {
    const fields = error instanceof ApiError ? Object.keys(error.fields ?? {}) : []
    if (input.category_id && fields.length === 1 && fields[0] === 'category_id') {
      return savePlanItem(id, { ...input, category_id: null })
    }
    throw error
  }
}

export function SuggestionList({
  plan,
  title,
  suggestions,
  fallback,
}: {
  plan: MonthPlan
  title: string
  suggestions: PlanSuggestion[]
  fallback?: RefObject<HTMLElement | null>
}) {
  const queryClient = useQueryClient()
  const invalidate = usePlanInvalidation()
  const undo = usePlanUndo()
  // The rows answered here that the server's list still holds: hidden while their
  // answer travels and until the refreshed list leaves them out. Each new list
  // forgets the ones it no longer holds, so one the server offers again later shows.
  const [answered, setAnswered] = useState<ReadonlySet<string>>(() => new Set())
  const [offered, setOffered] = useState(suggestions)
  if (suggestions !== offered) {
    setOffered(suggestions)
    const keys = new Set(suggestions.map((s) => s.key))
    if ([...answered].some((key) => !keys.has(key))) {
      setAnswered(new Set([...answered].filter((key) => keys.has(key))))
    }
  }
  const [expanded, setExpanded] = useState(false)
  // The item id each suggestion is added with, kept until it is: a retry sends it again.
  const ids = useRef(new Map<string, string>())
  // Where focus goes once the list re-renders: the row now at `index` after an
  // answer, or the row (by key) that came back after a refusal.
  const focusNext = useRef<{ index: number } | { key: string } | null>(null)
  const listRef = useRef<HTMLUListElement>(null)

  const left = suggestions.filter((s) => !answered.has(s.key))
  const shown = expanded ? left : left.slice(0, SHOWN)

  useEffect(() => {
    const target = focusNext.current
    if (!target) return
    focusNext.current = null
    const rows = Array.from(listRef.current?.children ?? [])
    const row =
      'key' in target
        ? rows.find((li) => li.getAttribute('data-key') === target.key)
        : rows[Math.min(target.index, rows.length - 1)]
    const button = row?.querySelector<HTMLButtonElement>('button')
    ;(button ?? fallback?.current)?.focus()
  }, [left.length, fallback])

  const mark = (key: string, value: boolean) =>
    setAnswered((previous) => {
      const next = new Set(previous)
      if (value) next.add(key)
      else next.delete(key)
      return next
    })

  /** Takes the row away while its answer travels; a refusal brings it back. */
  const answer = (
    s: PlanSuggestion,
    send: () => Promise<unknown>,
    done: () => void,
    refresh: () => Promise<unknown>,
  ) => {
    focusNext.current = { index: shown.indexOf(s) }
    mark(s.key, true)
    send()
      .then(done, (error: unknown) => {
        focusNext.current = { key: s.key }
        mark(s.key, false)
        toast.error(problemMessage(error))
      })
      .finally(() => void refresh())
  }

  const add = (s: PlanSuggestion) => {
    const month = plan.month
    const id = ids.current.get(s.key) ?? crypto.randomUUID()
    ids.current.set(s.key, id)
    answer(
      s,
      () => saveSuggested(id, suggestedItem(s, month)),
      () => {
        ids.current.delete(s.key)
        toast(`Listo: agregamos ${s.name}.`, {
          action: {
            label: 'Deshacer',
            onClick: () =>
              undo(async () => {
                await deletePlanItem(id, month, 'from').catch(unlessGone)
                mark(s.key, false)
              }),
          },
        })
      },
      invalidate,
    )
  }

  const dismiss = (s: PlanSuggestion) =>
    answer(
      s,
      () => dismissPlanSuggestion(s),
      () =>
        toast('Listo: no lo sugerimos más.', {
          action: {
            label: 'Deshacer',
            onClick: () =>
              undo(async () => {
                await undismissPlanSuggestion(s)
                mark(s.key, false)
              }),
          },
        }),
      // Turning one down changes no figure of the plan: only the suggestions.
      () => queryClient.invalidateQueries({ queryKey: planSuggestionsKey }),
    )

  if (left.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-small font-semibold">{title}</h3>
      <ul ref={listRef} className="flex flex-col gap-2">
        {shown.map((s) => (
          <SuggestionRow
            key={s.key}
            suggestion={s}
            onAdd={() => add(s)}
            onDismiss={() => dismiss(s)}
          />
        ))}
      </ul>
      {left.length > SHOWN && (
        <Button
          type="button"
          variant="ghost"
          className="-ml-3 self-start"
          aria-expanded={expanded}
          onClick={() => setExpanded((open) => !open)}
        >
          {expanded ? 'Ver menos' : 'Ver más'}
        </Button>
      )}
    </div>
  )
}

/**
 * One suggestion: its name and "$3.68 · cerca del día 18", then its two answers,
 * which wrap under it where the column is narrow (the hero's account). The name is
 * never cut short, since it is what both answers are about: a long one from a
 * statement wraps instead. The buttons' description is the suggestion, so "Agregar"
 * says what it adds.
 */
function SuggestionRow({
  suggestion: s,
  onAdd,
  onDismiss,
}: {
  suggestion: PlanSuggestion
  onAdd: () => void
  onDismiss: () => void
}) {
  const textId = useId()
  return (
    <li data-key={s.key} className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <div id={textId} className="flex min-w-0 flex-1 basis-40 flex-col">
        <span className="text-body font-medium text-pretty break-words">{s.name}</span>
        <span className="text-small font-medium text-pretty text-muted-foreground">
          {suggestionHint(s)}
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <Button type="button" variant="secondary" aria-describedby={textId} onClick={onAdd}>
          Agregar
        </Button>
        <Button
          type="button"
          variant="ghost"
          aria-describedby={textId}
          className="text-muted-foreground"
          onClick={onDismiss}
        >
          No
        </Button>
      </div>
    </li>
  )
}
