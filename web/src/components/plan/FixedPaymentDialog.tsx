/**
 * <FixedPaymentDialog plan item open onOpenChange returnTo? />
 *
 *   item      the payment to edit, or null for a new one
 *   returnTo  where focus goes on closing when what opened the dialog is gone: the
 *             payment's row, when "Editar pago fijo" in its sheet opened it
 *
 * "Nuevo pago fijo" / "Pago fijo": something that goes out every month (rent, the
 * power bill, a subscription, a monthly Yappy). ¿Qué es?, Monto, Día que sale (a day
 * the month lacks falls on its last), and, optional, Cómo aparece en tu estado: the
 * text its line carries, which is how the plan sees it went out and keeps it out of
 * what is free. Without it the plan never guesses: the payment keeps its money
 * aside until the owner says which movement it was. Categoría is optional.
 *
 * An edit asks "Desde este mes" or "Solo este mes", and so does "Quitar pago fijo",
 * a ghost button in the body: the toast undoes it, so it never asks first. Below lg
 * a bottom sheet, from lg a dialog, with "Agregar pago fijo" or "Guardar" as its one
 * primary. AddPurchaseDialog's rules otherwise: an id per opening, every field's
 * problem under it, any other refusal in an Alert.
 */
import { useId, useMemo, useRef, useState, type RefObject } from 'react'
import { toast } from 'sonner'

import { deletePlanItem, savePlanItem, type PlanItemInput } from '@/api/endpoints'
import type { MonthPlan, PlanFixedLine, PlanScope } from '@/api/types'
import { serverProblems } from '@/components/account/AddPurchaseDialog'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { categoryOptions } from '@/lib/categories'
import { useCategories } from '@/lib/queries'
import { amountProblem, amountToCents, isClean, type Errors } from '@/lib/validate'
import {
  CategoryField,
  DayField,
  dayProblem,
  focusFirstProblem,
  FormFailure,
  ITEM_FIELDS,
  itemInput,
  MAX_MATCH,
  MAX_NAME,
  MoneyField,
  nameProblem,
  planAmount,
  RemoveButton,
  ScopeField,
  SubmitButton,
  TextField,
  useOpenRound,
  useReturnFocusOr,
  type ItemField,
} from './forms'
import { useDeletePlanItem, usePlanUndo, useSavePlanItem } from './usePlan'
import { scopeNote } from './words'

export function FixedPaymentDialog({
  plan,
  item,
  open,
  onOpenChange,
  returnTo,
}: {
  plan: MonthPlan
  item: PlanFixedLine | null
  open: boolean
  onOpenChange: (open: boolean) => void
  returnTo?: RefObject<HTMLElement | null>
}) {
  const returnFocus = useReturnFocusOr(returnTo)
  const round = useOpenRound(open)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <FixedForm
        key={round}
        plan={plan}
        item={item}
        returnFocus={returnFocus}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

function FixedForm({
  plan,
  item,
  returnFocus,
  onDone,
}: {
  plan: MonthPlan
  item: PlanFixedLine | null
  returnFocus: ReturnType<typeof useReturnFocusOr>
  onDone: () => void
}) {
  const formId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const categories = useCategories()
  const options = useMemo(
    () => categoryOptions(categories.data?.categories, 'expense'),
    [categories.data],
  )
  const save = useSavePlanItem()
  const remove = useDeletePlanItem()
  const undo = usePlanUndo()
  const month = plan.month

  // One id for a new payment, however many tries it takes.
  const [id] = useState(() => item?.id ?? crypto.randomUUID())
  const [name, setName] = useState(item?.name ?? '')
  const [amount, setAmount] = useState(item?.amount.formatted ?? '')
  const [day, setDay] = useState(item?.day ? String(item.day) : '')
  const [match, setMatch] = useState(item?.match_text ?? '')
  const [category, setCategory] = useState(item?.category_id ?? '')
  // A payment planned for this month alone is edited for it alone unless asked.
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
    const fields = serverProblems(error, ITEM_FIELDS)
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
      day: dayProblem(day, 'Elige el día que sale.'),
    }
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem(formRef)
      return
    }
    const input: PlanItemInput = {
      kind: 'fixed',
      month,
      scope,
      name: name.trim(),
      amount: planAmount(amount),
      day: Number(day),
      match_text: match.trim(),
      category_id: category || null,
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
      title={item ? 'Pago fijo' : 'Nuevo pago fijo'}
      description={
        scopeNote(plan) ??
        (item ? undefined : 'Lo que sale solo cada mes: alquiler, luz, una suscripción.')
      }
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost" size="lg" className="lg:h-11">
              Cancelar
            </Button>
          </DialogClose>
          <SubmitButton form={formId} pending={save.isPending} buttonRef={submitRef}>
            {item ? 'Guardar' : 'Agregar pago fijo'}
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
          label="¿Qué es?"
          value={name}
          onChange={edit('name', setName)}
          error={errors.name}
          placeholder="Ej. Alquiler"
          maxLength={MAX_NAME}
          autoFocus={!item}
        />
        <div className="grid grid-cols-2 items-start gap-3">
          <MoneyField
            label="Monto"
            value={amount}
            onChange={edit('amount', setAmount)}
            error={errors.amount}
          />
          <DayField
            label="Día que sale"
            value={day}
            onChange={edit('day', setDay)}
            error={errors.day}
            description="Si el mes es más corto, cuenta el último."
          />
        </div>
        <TextField
          label="Cómo aparece en tu estado (opcional)"
          value={match}
          onChange={edit('match_text', setMatch)}
          error={errors.match_text}
          description="Así sabemos cuándo ya salió."
          maxLength={MAX_MATCH}
        />
        <CategoryField
          label="Categoría (opcional)"
          value={category}
          onChange={edit('category_id', setCategory)}
          options={options}
          none="Sin categoría"
          error={errors.category_id}
        />
        {item && <ScopeField month={month} value={scope} onChange={setScope} />}
        {item && (
          <RemoveButton onClick={takeOut} pending={remove.isPending}>
            Quitar pago fijo
          </RemoveButton>
        )}
        {failure && (
          <FormFailure title="No pudimos guardar el pago fijo" message={failure} />
        )}
      </form>
    </ResponsiveDialogContent>
  )
}

/** Whether the form still says what the payment already was: nothing to send. */
function unchanged(line: PlanFixedLine, input: PlanItemInput): boolean {
  return (
    line.name === input.name &&
    line.amount.cents === amountToCents(input.amount) &&
    line.day === input.day &&
    line.match_text === input.match_text &&
    (line.category_id ?? null) === input.category_id
  )
}
