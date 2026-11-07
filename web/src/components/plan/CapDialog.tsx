/**
 * <CapDialog plan cap open onOpenChange />
 *
 *   cap  the cap to edit, or null for a new one
 *
 * "Poner tope" / "Tope para Alimentación": how much of what is free one spending
 * category, the ones inside it included, should take each month. A cap is a view
 * inside what is free and never comes off it. Categoría (a new cap only: the expense
 * categories that have none yet) and Tope al mes, with what the category took in
 * the three months before this one: "De agosto a octubre gastaste unos $310.00 al
 * mes aquí." Edits and "Quitar tope" ask "Desde este mes" or "Solo este mes". "Guardar
 * tope" is its one primary; otherwise FixedPaymentDialog's rules.
 */
import { useId, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { deletePlanCap, savePlanCap } from '@/api/endpoints'
import type { CategoryNode, MonthPlan, PlanCapLine, PlanScope } from '@/api/types'
import { serverProblems } from '@/components/account/AddPurchaseDialog'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { categoryNames, categoryOptions } from '@/lib/categories'
import { useReturnFocus } from '@/lib/focus'
import { currentMonth, monthBounds, money, shiftMonth } from '@/lib/format'
import { useCategories, useCategoryTotals } from '@/lib/queries'
import { amountProblem, amountToCents, isClean, type Errors } from '@/lib/validate'
import {
  CategoryField,
  focusFirstProblem,
  FormFailure,
  MoneyField,
  planAmount,
  RemoveButton,
  ScopeField,
  SubmitButton,
  useOpenRound,
} from './forms'
import { useDeletePlanCap, usePlanUndo, useSavePlanCap } from './usePlan'
import { monthWord, scopeNote } from './words'

const CAP_FIELDS = ['category_id', 'amount', 'scope'] as const
type CapField = (typeof CAP_FIELDS)[number]

/** How many complete months the hint averages over. */
const HISTORY_MONTHS = 3

export function CapDialog({
  plan,
  cap,
  open,
  onOpenChange,
}: {
  plan: MonthPlan
  cap: PlanCapLine | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const returnFocus = useReturnFocus()
  const round = useOpenRound(open)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <CapForm
        key={round}
        plan={plan}
        cap={cap}
        returnFocus={returnFocus}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

function CapForm({
  plan,
  cap,
  returnFocus,
  onDone,
}: {
  plan: MonthPlan
  cap: PlanCapLine | null
  returnFocus: ReturnType<typeof useReturnFocus>
  onDone: () => void
}) {
  const formId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const categories = useCategories()
  const tree = categories.data?.categories
  const names = useMemo(() => categoryNames(tree), [tree])
  // A new cap goes on a spending category that has none yet.
  const options = useMemo(() => {
    const capped = new Set(plan.caps.map((c) => c.category_id))
    return categoryOptions(tree, 'expense').filter((o) => !capped.has(o.id))
  }, [tree, plan.caps])
  const save = useSavePlanCap()
  const remove = useDeletePlanCap()
  const undo = usePlanUndo()
  const month = plan.month

  const [category, setCategory] = useState(cap?.category_id ?? '')
  const [amount, setAmount] = useState(cap?.amount.formatted ?? '')
  // A cap set for this month alone is edited for it alone unless asked.
  const [scope, setScope] = useState<PlanScope>(
    cap && cap.from === month && cap.to === month ? 'only' : 'from',
  )
  const [errors, setErrors] = useState<Errors<CapField>>({})
  const [failure, setFailure] = useState<string | null>(null)
  const busy = save.isPending || remove.isPending
  const name = names.get(category) ?? 'esta categoría'

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (busy) return
    const found: Errors<CapField> = {
      category_id: category ? undefined : 'Elige una categoría.',
      amount: amountProblem(planAmount(amount)),
    }
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem(formRef)
      return
    }
    const text = planAmount(amount)
    if (cap && cap.amount.cents === amountToCents(text)) return onDone()
    save.mutate(
      { categoryId: category, input: { month, scope, amount: text } },
      {
        onSuccess: () => {
          const before = cap
          const what = `tope de ${money(amountToCents(text) ?? 0)} para ${name}.`
          toast(before ? `Guardado: ${what}` : `Listo: ${what}`, {
            action: {
              label: 'Deshacer',
              onClick: () =>
                undo(() =>
                  before
                    ? savePlanCap(category, {
                        month,
                        scope,
                        amount: before.amount.formatted,
                      })
                    : deletePlanCap(category, month, scope),
                ),
            },
          })
          onDone()
        },
        onError: (error) => {
          const fields = serverProblems(error, CAP_FIELDS)
          if (fields) {
            setErrors(fields)
            focusFirstProblem(formRef)
          } else {
            setFailure(problemMessage(error))
            focusWhenReady(() => submitRef.current)
          }
        },
      },
    )
  }

  const takeOut = () => {
    if (!cap || busy) return
    setFailure(null)
    remove.mutate(
      { categoryId: cap.category_id, month, scope },
      {
        onSuccess: () => {
          toast(`Listo: quitamos el tope de ${name}.`, {
            action: {
              label: 'Deshacer',
              onClick: () =>
                undo(() =>
                  savePlanCap(cap.category_id, {
                    month,
                    scope,
                    amount: cap.amount.formatted,
                  }),
                ),
            },
          })
          onDone()
        },
        onError: (error) => {
          setFailure(problemMessage(error))
          focusWhenReady(() => submitRef.current)
        },
      },
    )
  }

  return (
    <ResponsiveDialogContent
      {...returnFocus}
      title={cap ? `Tope para ${name}` : 'Poner tope'}
      description={
        scopeNote(plan) ??
        (cap ? undefined : 'Lo que una categoría puede llevarse de lo libre cada mes.')
      }
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost" size="lg" className="lg:h-11">
              Cancelar
            </Button>
          </DialogClose>
          <SubmitButton form={formId} pending={save.isPending} buttonRef={submitRef}>
            Guardar tope
          </SubmitButton>
        </>
      }
    >
      <form
        ref={formRef}
        id={formId}
        onSubmit={submit}
        noValidate
        className="flex flex-col gap-4"
      >
        {!cap && (
          <CategoryField
            label="Categoría"
            value={category}
            onChange={(value) => {
              setCategory(value)
              if (errors.category_id) setErrors((e) => ({ ...e, category_id: undefined }))
            }}
            options={options}
            none="Elige una categoría"
            error={errors.category_id}
          />
        )}
        <MoneyField
          label="Tope al mes"
          value={amount}
          onChange={(value) => {
            setAmount(value)
            if (errors.amount) setErrors((e) => ({ ...e, amount: undefined }))
          }}
          error={errors.amount}
          description={<CapHistory categoryId={category} tree={tree} />}
          autoFocus={Boolean(cap)}
        />
        {cap && <ScopeField month={month} value={scope} onChange={setScope} />}
        {cap && (
          <RemoveButton onClick={takeOut} pending={remove.isPending}>
            Quitar tope
          </RemoveButton>
        )}
        {failure && <FormFailure title="No pudimos guardar el tope" message={failure} />}
      </form>
    </ResponsiveDialogContent>
  )
}

/**
 * "De agosto a octubre gastaste unos $310.00 al mes aquí.": what the category and the
 * ones inside it took in each of the last complete months, on average, to the cent
 * like every other figure (aboutMoney's whole dollars are for the per-day line).
 * While that loads, when it fails, and when the category took less than a dollar,
 * what a cap counts instead. Its own component, so the months are fetched only once
 * the dialog is open (the form renders, closed, with the page).
 */
function CapHistory({
  categoryId,
  tree,
}: {
  categoryId: string
  tree: CategoryNode[] | undefined
}) {
  return useHistory(categoryId, tree) ?? 'Cuenta también las categorías que tiene dentro.'
}

function useHistory(categoryId: string, tree: CategoryNode[] | undefined): string | null {
  const now = currentMonth()
  const first = shiftMonth(now, -HISTORY_MONTHS)
  const last = shiftMonth(now, -1)
  const totals = useCategoryTotals({
    from: monthBounds(first).from,
    to: monthBounds(last).to,
    kind: 'spend',
  })
  if (!categoryId || !totals.data) return null
  const inside = new Set([categoryId])
  for (const node of tree ?? []) {
    if (node.id === categoryId) for (const child of node.children) inside.add(child.id)
  }
  const sum = totals.data.totals
    .filter((t) => t.category_id !== null && inside.has(t.category_id))
    .reduce((acc, t) => acc + t.total.cents, 0)
  const average = Math.floor(sum / HISTORY_MONTHS)
  if (average < 100) return null
  return `De ${monthWord(first)} a ${monthWord(last)} gastaste unos ${money(average)} al mes aquí.`
}
