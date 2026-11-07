/**
 * <GoalDialog plan item open onOpenChange />
 *
 *   item  the saving to edit, or null for a new one
 *
 * "Nuevo ahorro" / "Ahorro": what the owner sets aside each month. ¿Para qué?, Aporte
 * al mes, and, optional, Meta (how much they want to get to), ¿Para cuándo? (the month
 * to get there by, which needs a goal) and ¿Dónde lo guardas?: one of their savings
 * or brokerage accounts, where the money goes as a transfer (never spending). The
 * contribution is always taken out of what is free. With an account, the Ahorro card
 * reads it: what reached it this month, and what it holds against the goal; with a
 * goal and a date, about how much each month to it would take.
 *
 * Only the contribution changes by month; the name, the goal, its date and the
 * account hold for every month of the saving. An account backs one goal at a time:
 * the server's refusal is said under that field. Edits and "Quitar ahorro" ask "Desde
 * este mes" or "Solo este mes". Otherwise FixedPaymentDialog's rules.
 */
import { useId, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import { deletePlanItem, savePlanItem, type PlanItemInput } from '@/api/endpoints'
import type { MonthPlan, PlanSavingLine, PlanScope } from '@/api/types'
import { serverProblems } from '@/components/account/AddPurchaseDialog'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { useReturnFocus } from '@/lib/focus'
import { shiftMonth } from '@/lib/format'
import { useAccounts } from '@/lib/queries'
import { amountProblem, amountToCents, isClean, type Errors } from '@/lib/validate'
import {
  focusFirstProblem,
  FormFailure,
  itemInput,
  MAX_NAME,
  MoneyField,
  MonthField,
  monthsFrom,
  nameProblem,
  optionalAmountProblem,
  planAmount,
  RemoveButton,
  SavingAccountField,
  savingAccounts,
  ScopeField,
  SubmitButton,
  TextField,
  useOpenRound,
} from './forms'
import { useDeletePlanItem, usePlanUndo, useSavePlanItem } from './usePlan'
import { scopeNote } from './words'

/** The fields this form shows, by the names the server reports problems under. */
const FIELDS = ['name', 'amount', 'target', 'target_month', 'account_id', 'scope'] as const
type GoalField = (typeof FIELDS)[number]

/**
 * How far ahead a goal's date can be picked: five years. A goal can be further off
 * than anything the plan sets aside month by month, and the server puts no limit on
 * it; a longer list would only be harder to scroll.
 */
const GOAL_MONTHS = 60

export function GoalDialog({
  plan,
  item,
  open,
  onOpenChange,
}: {
  plan: MonthPlan
  item: PlanSavingLine | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const returnFocus = useReturnFocus()
  const round = useOpenRound(open)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <GoalForm
        key={round}
        plan={plan}
        item={item}
        returnFocus={returnFocus}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

function GoalForm({
  plan,
  item,
  returnFocus,
  onDone,
}: {
  plan: MonthPlan
  item: PlanSavingLine | null
  returnFocus: ReturnType<typeof useReturnFocus>
  onDone: () => void
}) {
  const formId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const accounts = useAccounts()
  const choices = useMemo(() => savingAccounts(accounts.data?.accounts), [accounts.data])
  const save = useSavePlanItem()
  const remove = useDeletePlanItem()
  const undo = usePlanUndo()
  const month = plan.month
  const months = monthsFrom(month, shiftMonth(month, GOAL_MONTHS))

  const [id] = useState(() => item?.id ?? crypto.randomUUID())
  const [name, setName] = useState(item?.name ?? '')
  const [amount, setAmount] = useState(item?.amount.formatted ?? '')
  const [target, setTarget] = useState(item?.target?.formatted ?? '')
  const [targetMonth, setTargetMonth] = useState(item?.target_month ?? '')
  const [account, setAccount] = useState(item?.account_id ?? '')
  // A saving planned for this month alone is edited for it alone unless asked.
  const [scope, setScope] = useState<PlanScope>(
    item && item.from === month && item.to === month ? 'only' : 'from',
  )
  const [errors, setErrors] = useState<Errors<GoalField>>({})
  const [failure, setFailure] = useState<string | null>(null)
  const busy = save.isPending || remove.isPending

  const edit = (field: GoalField, set: (value: string) => void) => (value: string) => {
    set(value)
    if (errors[field]) setErrors((current) => ({ ...current, [field]: undefined }))
  }

  const refused = (error: unknown) => {
    // One account, one goal: said where the account is chosen.
    const fields =
      error instanceof ApiError && error.code === 'account_has_goal'
        ? { account_id: error.message }
        : serverProblems(error, FIELDS)
    if (fields) {
      setErrors(fields)
      focusFirstProblem(formRef)
    } else {
      setFailure(problemMessage(error))
      focusWhenReady(() => submitRef.current)
    }
  }

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (busy) return
    const goal = planAmount(target)
    const found: Errors<GoalField> = {
      name: nameProblem(name),
      amount: amountProblem(planAmount(amount)),
      target: optionalAmountProblem(goal),
      // A date to reach nothing says nothing: the month needs a goal.
      target_month: targetMonth && !goal ? 'Pon una meta para darle fecha.' : undefined,
    }
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem(formRef)
      return
    }
    const input: PlanItemInput = {
      // What this dialog does not show (a day) goes back as it was, so editing the
      // contribution never drops it.
      ...(item ? itemInput(item, month, scope) : { kind: 'saving' as const, month, scope }),
      name: name.trim(),
      amount: planAmount(amount),
      account_id: account || null,
      match_text: '',
      category_id: null,
      // Left out, the saving has none: a goal cleared here is cleared.
      target: goal || undefined,
      target_month: targetMonth || undefined,
    }
    if (item && unchanged(item, input)) return onDone()
    save.mutate(
      { id, input },
      {
        onSuccess: () => {
          const before = item
          toast(before ? `Guardado: ${input.name}.` : `Listo: agregamos ${input.name}.`, {
            action: {
              label: 'Deshacer',
              onClick: () =>
                undo(() =>
                  before
                    ? savePlanItem(id, itemInput(before, month, scope))
                    : deletePlanItem(id, month, scope),
                ),
            },
          })
          onDone()
        },
        onError: refused,
      },
    )
  }

  const takeOut = () => {
    if (!item || busy) return
    setFailure(null)
    remove.mutate(
      { id: item.id, month, scope },
      {
        onSuccess: () => {
          toast(`Listo: quitamos ${item.name}.`, {
            action: {
              label: 'Deshacer',
              onClick: () =>
                undo(() => savePlanItem(item.id, itemInput(item, month, scope))),
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
      title={item ? 'Ahorro' : 'Nuevo ahorro'}
      description={
        scopeNote(plan) ??
        (item ? undefined : 'Lo que apartas cada mes, aunque sea poquito.')
      }
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost" size="lg" className="lg:h-11">
              Cancelar
            </Button>
          </DialogClose>
          <SubmitButton form={formId} pending={save.isPending} buttonRef={submitRef}>
            {item ? 'Guardar' : 'Agregar ahorro'}
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
        <TextField
          label="¿Para qué?"
          value={name}
          onChange={edit('name', setName)}
          error={errors.name}
          placeholder="Ej. Fondo de emergencia"
          maxLength={MAX_NAME}
          autoFocus={!item}
        />
        <MoneyField
          label="Aporte al mes"
          value={amount}
          onChange={edit('amount', setAmount)}
          error={errors.amount}
          description="Sale de lo libre cada mes."
        />
        <div className="grid grid-cols-2 items-start gap-3">
          <MoneyField
            label="Meta (opcional)"
            value={target}
            onChange={edit('target', setTarget)}
            error={errors.target}
          />
          <MonthField
            label="¿Para cuándo?"
            value={targetMonth}
            onChange={edit('target_month', setTargetMonth)}
            months={months}
            none="Sin fecha"
            error={errors.target_month}
          />
        </div>
        <SavingAccountField
          value={account}
          onChange={edit('account_id', setAccount)}
          choices={choices}
          error={errors.account_id}
        />
        {item && <ScopeField month={month} value={scope} onChange={setScope} />}
        {item && (
          <RemoveButton onClick={takeOut} pending={remove.isPending}>
            Quitar ahorro
          </RemoveButton>
        )}
        {failure && <FormFailure title="No pudimos guardar el ahorro" message={failure} />}
      </form>
    </ResponsiveDialogContent>
  )
}

/** Whether the form still says what the saving already was: nothing to send. */
function unchanged(line: PlanSavingLine, input: PlanItemInput): boolean {
  return (
    line.name === input.name &&
    line.amount.cents === amountToCents(input.amount) &&
    (line.account_id ?? null) === input.account_id &&
    (line.target?.cents ?? null) === (input.target ? amountToCents(input.target) : null) &&
    (line.target_month ?? '') === (input.target_month ?? '')
  )
}
