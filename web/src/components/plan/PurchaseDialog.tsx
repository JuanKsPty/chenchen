/**
 * <PurchaseDialog plan item open onOpenChange returnTo? />
 *
 *   item      the purchase to edit, or null for a new one
 *   returnTo  where focus goes on closing when what opened the dialog is gone: the
 *             purchase's row, when "Editar compra" in its sheet opened it
 *
 * "Planear compra" / "Compra planeada": something the owner means to buy by a month,
 * with its money set aside in the plan beforehand. ¿Qué vas a comprar?, Monto, ¿Para
 * cuándo? (from the month on screen to 24 months after the current one, as far as the
 * server plans) and Cómo apartarlo:
 *   - "Un poco cada mes": a share of the price comes off what is free every month
 *     from this one to that one
 *   - "Todo ese mes": all of it comes off that month
 *   - "Con mis ahorros": none of it does; what the owner saved pays it
 * Under them, live, what that sets aside, in the cents the server will write
 * (setAsideHint): "Apartas $220.00 al mes de noviembre a marzo." It is a figure of the
 * plan, never a balance: no account holds it. Optional, as for a fixed payment: a
 * category, and how it appears in the statement, which is how the plan recognises the
 * outflow that bought it.
 *
 * A purchase is always edited from the month on screen on: the server refuses one
 * month alone, since its shares must still add up to its price, so there is no "¿Para
 * qué meses?". The months before keep what they set aside, and what is left of the
 * price is spread again from this one. "Quitar compra" is a ghost button in the body;
 * its toast's "Deshacer" saves the purchase again as it was, which plans it again from
 * this month on. FixedPaymentDialog's rules otherwise: an id per opening, every
 * field's problem under it, any other refusal in an Alert.
 */
import { useId, useMemo, useRef, useState, type RefObject } from 'react'
import { toast } from 'sonner'

import { deletePlanItem, savePlanItem, type PlanItemInput } from '@/api/endpoints'
import type { MonthPlan, PlanFunding, PlanPurchaseLine } from '@/api/types'
import { serverProblems } from '@/components/account/AddPurchaseDialog'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { FieldError, FieldLegend, FieldSet } from '@/components/ui/field'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { categoryOptions } from '@/lib/categories'
import { shiftMonth } from '@/lib/format'
import { useCategories } from '@/lib/queries'
import { amountProblem, amountToCents, isClean, type Errors } from '@/lib/validate'
import {
  CategoryField,
  focusFirstProblem,
  FormFailure,
  MAX_MATCH,
  MAX_NAME,
  MoneyField,
  MonthField,
  monthsFrom,
  nameProblem,
  planAmount,
  RemoveButton,
  SubmitButton,
  TextField,
  unlessGone,
  useOpenRound,
  useReturnFocusOr,
} from './forms'
import { leftToSetAside } from './shares'
import { useDeletePlanItem, usePlanUndo, useSavePlanItem } from './usePlan'
import { scopeNote, setAsideHint } from './words'

/** How far ahead a purchase can be planned: the server's reach from the current month. */
const MONTHS_AHEAD = 24

/** The fields this form shows, by the names the server reports problems under. */
const FIELDS = [
  'name',
  'amount',
  'target_month',
  'funding',
  'match_text',
  'category_id',
] as const
type PurchaseField = (typeof FIELDS)[number]

const FUNDINGS: { value: PlanFunding; label: string }[] = [
  { value: 'monthly', label: 'Un poco cada mes' },
  { value: 'that_month', label: 'Todo ese mes' },
  { value: 'savings', label: 'Con mis ahorros' },
]

/**
 * What a purchase was, as the form would send it again from `month` on: the undo of
 * an edit or of taking it out. Never its start month, which the server keeps.
 */
function purchaseInput(line: PlanPurchaseLine, month: string): PlanItemInput {
  return {
    kind: 'purchase',
    month,
    scope: 'from',
    name: line.name,
    amount: line.amount.formatted,
    match_text: line.match_text,
    category_id: line.category_id ?? null,
    target_month: line.target_month,
    funding: line.funding,
  }
}

export function PurchaseDialog({
  plan,
  item,
  open,
  onOpenChange,
  returnTo,
}: {
  plan: MonthPlan
  item: PlanPurchaseLine | null
  open: boolean
  onOpenChange: (open: boolean) => void
  returnTo?: RefObject<HTMLElement | null>
}) {
  const returnFocus = useReturnFocusOr(returnTo)
  const round = useOpenRound(open)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <PurchaseForm
        key={round}
        plan={plan}
        item={item}
        returnFocus={returnFocus}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

function PurchaseForm({
  plan,
  item,
  returnFocus,
  onDone,
}: {
  plan: MonthPlan
  item: PlanPurchaseLine | null
  returnFocus: ReturnType<typeof useReturnFocusOr>
  onDone: () => void
}) {
  const formId = useId()
  const fundingId = useId()
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
  // From the month on screen to 24 after the current one, by the server's calendar.
  const months = monthsFrom(month, shiftMonth(plan.today.slice(0, 7), MONTHS_AHEAD))

  // One id for a new purchase, however many tries it takes.
  const [id] = useState(() => item?.id ?? crypto.randomUUID())
  const [name, setName] = useState(item?.name ?? '')
  const [amount, setAmount] = useState(item?.amount.formatted ?? '')
  // A month before the one on screen cannot be saved from it, so it is chosen again.
  const [target, setTarget] = useState(
    item && item.target_month >= month ? item.target_month : '',
  )
  const [funding, setFunding] = useState<PlanFunding>(item?.funding ?? 'monthly')
  const [match, setMatch] = useState(item?.match_text ?? '')
  const [category, setCategory] = useState(item?.category_id ?? '')
  const [errors, setErrors] = useState<Errors<PurchaseField>>({})
  const [failure, setFailure] = useState<string | null>(null)
  const busy = save.isPending || remove.isPending

  const edit = (field: PurchaseField, set: (value: string) => void) => (value: string) => {
    set(value)
    if (errors[field]) setErrors((current) => ({ ...current, [field]: undefined }))
  }

  // What this sets aside, worked out as the server will: what is left of the price
  // after the months before this one, from this month to the one chosen.
  const price = amountToCents(planAmount(amount))
  const hint = setAsideHint(
    funding,
    price && price > 0 ? leftToSetAside(price, item) : null,
    month,
    target,
  )

  const refused = (error: unknown) => {
    const fields = serverProblems(error, FIELDS)
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
    const found: Errors<PurchaseField> = {
      name: nameProblem(name),
      amount: amountProblem(planAmount(amount)),
      target_month: target ? undefined : 'Elige para cuándo.',
    }
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem(formRef)
      return
    }
    const input: PlanItemInput = {
      kind: 'purchase',
      month,
      scope: 'from',
      name: name.trim(),
      amount: planAmount(amount),
      match_text: match.trim(),
      category_id: category || null,
      target_month: target,
      funding,
    }
    if (item && unchanged(item, input)) return onDone()
    save.mutate(
      { id, input },
      {
        onSuccess: () => {
          const before = item
          toast(before ? `Guardado: ${input.name}.` : `Listo: planeamos ${input.name}.`, {
            action: {
              label: 'Deshacer',
              onClick: () =>
                undo(() =>
                  before
                    ? savePlanItem(id, purchaseInput(before, month))
                    : deletePlanItem(id, month, 'from').catch(unlessGone),
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
      { id: item.id, month, scope: 'from' },
      {
        onSuccess: () => {
          toast(`Listo: quitamos ${item.name}.`, {
            action: {
              label: 'Deshacer',
              onClick: () => undo(() => savePlanItem(item.id, purchaseInput(item, month))),
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

  // The months before this one keep what they set aside: worth saying only when there
  // are any.
  const keeps =
    item && item.start_month < month
      ? 'Los meses anteriores quedan como estaban.'
      : undefined

  return (
    <ResponsiveDialogContent
      {...returnFocus}
      title={item ? 'Compra planeada' : 'Planear compra'}
      description={
        scopeNote(plan) ?? (item ? keeps : 'La apartamos en tu plan, no en una cuenta.')
      }
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost" size="lg" className="lg:h-11">
              Cancelar
            </Button>
          </DialogClose>
          <SubmitButton form={formId} pending={save.isPending} buttonRef={submitRef}>
            {item ? 'Guardar' : 'Planear compra'}
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
          label="¿Qué vas a comprar?"
          value={name}
          onChange={edit('name', setName)}
          error={errors.name}
          placeholder="Ej. Laptop"
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
          <MonthField
            label="¿Para cuándo?"
            value={target}
            onChange={edit('target_month', setTarget)}
            months={months}
            none="Elige el mes"
            error={errors.target_month}
          />
        </div>

        <FieldSet className="gap-1" data-invalid={errors.funding ? true : undefined}>
          <FieldLegend id={fundingId} variant="label">
            Cómo apartarlo
          </FieldLegend>
          <RadioGroup
            value={funding}
            onValueChange={(next) => {
              const chosen = FUNDINGS.find((f) => f.value === next)
              if (chosen) setFunding(chosen.value)
              if (errors.funding)
                setErrors((current) => ({ ...current, funding: undefined }))
            }}
            aria-labelledby={fundingId}
            aria-describedby={`${fundingId}-hint`}
          >
            {FUNDINGS.map((f) => (
              <label
                key={f.value}
                className="-mx-3 flex min-h-11 cursor-pointer items-center gap-3 rounded-2xl px-3 py-2 hover:bg-muted"
              >
                <RadioGroupItem
                  value={f.value}
                  aria-invalid={errors.funding ? true : undefined}
                />
                <span className="text-body font-medium text-foreground">{f.label}</span>
              </label>
            ))}
          </RadioGroup>
          {/* Live: it follows the price, the month and the way chosen. */}
          <p
            id={`${fundingId}-hint`}
            aria-live="polite"
            className="text-small font-medium text-pretty text-muted-foreground"
          >
            {hint}
          </p>
          {errors.funding && <FieldError>{errors.funding}</FieldError>}
        </FieldSet>

        <TextField
          label="Cómo aparece en tu estado (opcional)"
          value={match}
          onChange={edit('match_text', setMatch)}
          error={errors.match_text}
          description="Así sabemos cuándo la compraste."
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
        {item && (
          <RemoveButton onClick={takeOut} pending={remove.isPending}>
            Quitar compra
          </RemoveButton>
        )}
        {failure && <FormFailure title="No pudimos guardar la compra" message={failure} />}
      </form>
    </ResponsiveDialogContent>
  )
}

/** Whether the form still says what the purchase already was: nothing to send. */
function unchanged(line: PlanPurchaseLine, input: PlanItemInput): boolean {
  return (
    line.name === input.name &&
    line.amount.cents === amountToCents(input.amount) &&
    line.match_text === input.match_text &&
    (line.category_id ?? null) === input.category_id &&
    line.target_month === input.target_month &&
    line.funding === input.funding
  )
}
