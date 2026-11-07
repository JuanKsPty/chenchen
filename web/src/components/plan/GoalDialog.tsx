/**
 * <GoalDialog plan item open onOpenChange />
 *
 *   item  the saving to edit, or null for a new one
 *
 * "Nuevo ahorro" / "Ahorro": what the owner sets aside each month. ¿Para qué?, Aporte
 * al mes and, optional, ¿Dónde lo guardas?: one of their savings or brokerage
 * accounts, where the money goes as a transfer (never spending). The contribution
 * is always taken out of what is free. A goal and its date come later; a saving
 * that already has them keeps them when it is edited here.
 *
 * An account backs one goal at a time: the server's refusal is said under that
 * field. Edits and "Quitar ahorro" ask "Desde este mes" or "Solo este mes". Otherwise
 * FixedPaymentDialog's rules.
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
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { shortAccountLabel } from '@/lib/account'
import { useReturnFocus } from '@/lib/focus'
import { useAccounts } from '@/lib/queries'
import { amountProblem, amountToCents, isClean, type Errors } from '@/lib/validate'
import {
  focusFirstProblem,
  FormFailure,
  ITEM_FIELDS,
  itemInput,
  MAX_NAME,
  MoneyField,
  nameProblem,
  planAmount,
  RemoveButton,
  ScopeField,
  SubmitButton,
  TextField,
  useOpenRound,
  type ItemField,
} from './forms'
import { useDeletePlanItem, usePlanUndo, useSavePlanItem } from './usePlan'
import { scopeNote } from './words'

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
  const accountFieldId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const accounts = useAccounts()
  // Where savings are kept: the owner's savings and brokerage accounts.
  const choices = useMemo(
    () =>
      (accounts.data?.accounts ?? [])
        .filter((a) => (a.type === 'savings' || a.type === 'brokerage') && !a.paid_by_other)
        .map((a) => ({ id: a.id, name: a.alias?.trim() || shortAccountLabel(a) })),
    [accounts.data],
  )
  const save = useSavePlanItem()
  const remove = useDeletePlanItem()
  const undo = usePlanUndo()
  const month = plan.month

  const [id] = useState(() => item?.id ?? crypto.randomUUID())
  const [name, setName] = useState(item?.name ?? '')
  const [amount, setAmount] = useState(item?.amount.formatted ?? '')
  const [account, setAccount] = useState(item?.account_id ?? '')
  // A saving planned for this month alone is edited for it alone unless asked.
  const [scope, setScope] = useState<PlanScope>(
    item && item.from === month && item.to === month ? 'only' : 'from',
  )
  const [errors, setErrors] = useState<Errors<ItemField>>({})
  const [failure, setFailure] = useState<string | null>(null)
  const busy = save.isPending || remove.isPending

  const edit = (field: ItemField, set: (value: string) => void) => (value: string) => {
    set(value)
    if (errors[field]) setErrors((current) => ({ ...current, [field]: undefined }))
  }

  const refused = (error: unknown) => {
    // One account, one goal: said where the account is chosen.
    const fields =
      error instanceof ApiError && error.code === 'account_has_goal'
        ? { account_id: error.message }
        : serverProblems(error, ITEM_FIELDS)
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
    const found: Errors<ItemField> = {
      name: nameProblem(name),
      amount: amountProblem(planAmount(amount)),
    }
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem(formRef)
      return
    }
    const input: PlanItemInput = {
      // What this dialog does not show yet (a day, a goal and its month) goes back
      // as it was, so editing the contribution never drops it.
      ...(item ? itemInput(item, month, scope) : { kind: 'saving' as const, month, scope }),
      name: name.trim(),
      amount: planAmount(amount),
      account_id: account || null,
      match_text: '',
      category_id: null,
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
        <Field data-invalid={errors.account_id ? true : undefined}>
          <FieldLabel htmlFor={accountFieldId}>¿Dónde lo guardas? (opcional)</FieldLabel>
          <NativeSelect
            id={accountFieldId}
            className="w-full"
            value={account}
            aria-invalid={errors.account_id ? true : undefined}
            aria-describedby={
              errors.account_id
                ? `${accountFieldId}-hint ${accountFieldId}-error`
                : `${accountFieldId}-hint`
            }
            onChange={(event) => edit('account_id', setAccount)(event.target.value)}
          >
            <NativeSelectOption value="">En ninguna en particular</NativeSelectOption>
            {choices.map((choice) => (
              <NativeSelectOption key={choice.id} value={choice.id}>
                {choice.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <FieldDescription id={`${accountFieldId}-hint`}>
            Una cuenta de ahorros o de inversión tuya.
          </FieldDescription>
          {errors.account_id && (
            <FieldError id={`${accountFieldId}-error`}>{errors.account_id}</FieldError>
          )}
        </Field>
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

function unchanged(line: PlanSavingLine, input: PlanItemInput): boolean {
  return (
    line.name === input.name &&
    line.amount.cents === amountToCents(input.amount) &&
    (line.account_id ?? null) === input.account_id
  )
}
