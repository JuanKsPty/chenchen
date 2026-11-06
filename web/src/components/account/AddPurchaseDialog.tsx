/**
 * <AddPurchaseDialog account open onOpenChange />
 *
 * "Anotar compra": a purchase the owner types by hand in this account. Monto, ¿Dónde?
 * and Día (today unless changed), and if they like a Categoría and a Nota. It counts
 * in their spending until the statement that prints it arrives, and never in the
 * balance. Left without a category, their rules file it by where it was, as they
 * would its line. When that line is already imported the server links the two at
 * once, and the toast says it was not counted twice.
 *
 * Opened from the account's overflow menu (never a brokerage account's), or from the
 * "Anotar compra" button of a card someone else pays, so it is controlled and gives
 * focus back to whatever opened it (a menu item's menu button). On such a card no
 * statement ever arrives, so it says "Cuenta en tus gastos de este mes." instead.
 * Below lg a bottom sheet, from lg a dialog (ResponsiveDialogContent), with "Anotar
 * compra" as its one primary. A field the server refuses is marked under that field,
 * with focus on the first one; any other refusal is an Alert above the buttons.
 *
 * Each opening picks the purchase's id and sends it on every try, so pressing "Anotar
 * compra" again after a send that got no answer never counts the purchase twice: the
 * server returns the one the first try noted.
 *
 * It also holds what "Corregir" in Categorizar shares with it: PurchaseFields (Monto,
 * ¿Dónde?, Día), purchaseProblems, purchaseAmount, serverProblems and purchaseToast.
 */
import { CircleAlertIcon } from 'lucide-react'
import { useId, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { Account, Capture } from '@/api/types'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { DateField } from '@/components/DateField'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { paidByOther } from '@/lib/account'
import { categoryOptions } from '@/lib/categories'
import { useReturnFocus } from '@/lib/focus'
import { signedMoney, todayCivil } from '@/lib/format'
import { useAddCapture, useCategories } from '@/lib/queries'
import { amountProblem, isClean, type Errors } from '@/lib/validate'

/** The server's limits, felt while typing; it still checks them. */
const MAX_MERCHANT = 140
const MAX_NOTE = 500

/** The server's own words for a purchase dated after today. */
const DAY_IN_FUTURE = 'El día no puede ser después de hoy.'

/** What the owner types about a purchase, as typed. */
export interface PurchaseValues {
  amount: string
  merchant: string
  /** YYYY-MM-DD, or empty when the field is. */
  day: string
}

export type PurchaseField = keyof PurchaseValues

/** The fields "Anotar compra" shows, by the names the server reports problems under. */
const ADD_FIELDS = ['amount', 'merchant', 'day', 'category_id', 'note'] as const
type AddField = (typeof ADD_FIELDS)[number]

/**
 * The amount as it is sent: what was typed, without a "$" or spaces. A comma stays,
 * for amountProblem to explain; read as a thousands separator it could turn 12,50
 * into $1,250.00.
 */
export function purchaseAmount(text: string): string {
  return text.replace(/[$\s]/g, '')
}

/**
 * What is wrong with a purchase before it is sent, field by field. `dayIncomplete` is
 * DateField's: text in the field that is not a date yet, which `values.day` no longer
 * matches.
 */
export function purchaseProblems(
  values: PurchaseValues,
  dayIncomplete: boolean,
): Errors<PurchaseField> {
  let day: string | undefined
  if (dayIncomplete) day = 'Escribe el día completo: dd/mm/aaaa.'
  else if (!values.day) day = 'Escribe el día de la compra.'
  else if (values.day > todayCivil()) day = DAY_IN_FUTURE
  return {
    amount: amountProblem(purchaseAmount(values.amount)),
    merchant: values.merchant.trim() ? undefined : 'Escribe dónde fue la compra.',
    day,
  }
}

/**
 * The server's messages for the fields a form shows, or null when it sent none of
 * them and the problem is the request's (say it in an Alert instead).
 */
export function serverProblems<T extends string>(
  error: unknown,
  fields: readonly T[],
): Errors<T> | null {
  if (!(error instanceof ApiError) || !error.fields) return null
  const found: Errors<T> = {}
  for (const field of fields) {
    const message = error.fields[field]
    if (message) found[field] = message
  }
  return isClean(found) ? null : found
}

/**
 * Says what became of a purchase the server just took. A pending one counts, and
 * `done` says so. One its statement already printed counts once, as the line. One a
 * statement has gone past without printing does not count.
 */
export function purchaseToast(capture: Capture, done: string) {
  switch (capture.state) {
    case 'linked':
    case 'printed':
      toast('Ya estaba en tu estado: no la contamos dos veces.')
      return
    case 'missing':
      toast('Anotada, pero tu estado de esos días no la trae: no la contamos.')
      return
    default:
      toast(done)
  }
}

/**
 * Monto, ¿Dónde? and Día, each with its problem under it (and aria-invalid, so a
 * form can find the first one to focus). Día is today at the latest.
 */
export function PurchaseFields({
  values,
  errors,
  onChange,
  onDayIncompleteChange,
  autoFocus = false,
}: {
  values: PurchaseValues
  errors: Errors<PurchaseField>
  onChange: (field: PurchaseField, value: string) => void
  /** Pass a state setter: DateField calls it from an effect. */
  onDayIncompleteChange: (incomplete: boolean) => void
  /** Focus Monto on mount: for a form that replaces another in the same dialog. */
  autoFocus?: boolean
}) {
  const id = useId()
  const amountId = `${id}-amount`
  const merchantId = `${id}-merchant`

  return (
    <>
      <Field data-invalid={errors.amount ? true : undefined}>
        <FieldLabel htmlFor={amountId}>Monto</FieldLabel>
        <Input
          id={amountId}
          value={values.amount}
          inputMode="decimal"
          autoComplete="off"
          placeholder="0.00"
          autoFocus={autoFocus}
          aria-invalid={errors.amount ? true : undefined}
          aria-describedby={errors.amount ? `${amountId}-error` : undefined}
          onChange={(event) => onChange('amount', event.target.value)}
        />
        {errors.amount && <FieldError id={`${amountId}-error`}>{errors.amount}</FieldError>}
      </Field>

      <Field data-invalid={errors.merchant ? true : undefined}>
        <FieldLabel htmlFor={merchantId}>¿Dónde?</FieldLabel>
        <Input
          id={merchantId}
          value={values.merchant}
          maxLength={MAX_MERCHANT}
          autoComplete="off"
          autoCapitalize="sentences"
          placeholder="Ej. Super 99"
          aria-invalid={errors.merchant ? true : undefined}
          aria-describedby={errors.merchant ? `${merchantId}-error` : undefined}
          onChange={(event) => onChange('merchant', event.target.value)}
        />
        {errors.merchant && (
          <FieldError id={`${merchantId}-error`}>{errors.merchant}</FieldError>
        )}
      </Field>

      <DateField
        label="Día"
        value={values.day}
        onChange={(day) => onChange('day', day)}
        onIncompleteChange={onDayIncompleteChange}
        max={todayCivil()}
        maxProblem={DAY_IN_FUTURE}
        error={errors.day}
      />
    </>
  )
}

export function AddPurchaseDialog({
  account,
  open,
  onOpenChange,
}: {
  account: Account
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const returnFocus = useReturnFocus()
  // A blank form every time it opens. Counted on opening, not on closing, so what
  // was typed stays put while the dialog animates out.
  const [round, setRound] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setRound((n) => n + 1)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <PurchaseForm
        key={round}
        account={account}
        returnFocus={returnFocus}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

function PurchaseForm({
  account,
  returnFocus,
  onDone,
}: {
  account: Account
  returnFocus: ReturnType<typeof useReturnFocus>
  onDone: () => void
}) {
  const formId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const categories = useCategories()
  const add = useAddCapture()
  // A purchase is spending, so only the spending categories.
  const options = useMemo(
    () => categoryOptions(categories.data?.categories, 'expense'),
    [categories.data],
  )

  const [values, setValues] = useState<PurchaseValues>(() => ({
    amount: '',
    merchant: '',
    day: todayCivil(),
  }))
  const [dayIncomplete, setDayIncomplete] = useState(false)
  const [categoryId, setCategoryId] = useState('')
  const [note, setNote] = useState('')
  const [errors, setErrors] = useState<Errors<AddField>>({})
  const [failure, setFailure] = useState<string | null>(null)
  // One id for this opening's purchase, however many tries it takes.
  const [purchaseId] = useState(() => crypto.randomUUID())

  const clear = (field: AddField) => {
    if (errors[field]) setErrors((current) => ({ ...current, [field]: undefined }))
  }
  const change = (field: PurchaseField, value: string) => {
    setValues((current) => ({ ...current, [field]: value }))
    clear(field)
  }
  const focusFirstProblem = () =>
    focusWhenReady(() =>
      formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]'),
    )

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (add.isPending) return
    const found = purchaseProblems(values, dayIncomplete)
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem()
      return
    }
    add.mutate(
      {
        id: purchaseId,
        account_id: account.id,
        amount: purchaseAmount(values.amount),
        merchant: values.merchant.trim(),
        day: values.day,
        category_id: categoryId || undefined,
        note: note.trim() || undefined,
      },
      {
        onSuccess: (capture) => {
          purchaseToast(
            capture,
            `Listo: ${signedMoney(capture.amount)} en ${capture.description}`,
          )
          onDone()
        },
        onError: (error) => {
          const fields = serverProblems(error, ADD_FIELDS)
          if (fields) {
            setErrors(fields)
            focusFirstProblem()
          } else {
            setFailure(problemMessage(error))
            focusWhenReady(() => submitRef.current)
          }
        },
      },
    )
  }

  const categoryFieldId = `${formId}-category`
  const noteId = `${formId}-note`

  return (
    <ResponsiveDialogContent
      {...returnFocus}
      title="Anotar compra"
      description={
        paidByOther(account)
          ? 'Cuenta en tus gastos de este mes.'
          : 'Cuenta en tus gastos hasta que llegue tu estado.'
      }
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost" size="lg" className="lg:h-11">
              Cancelar
            </Button>
          </DialogClose>
          <Button
            ref={submitRef}
            type="submit"
            form={formId}
            size="lg"
            className="lg:h-11"
            // Not disabled: that would drop the focus while it saves, and an error
            // then leaves it nowhere.
            aria-disabled={add.isPending || undefined}
          >
            {add.isPending && <Spinner aria-hidden="true" />}
            Anotar compra
          </Button>
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
        <PurchaseFields
          values={values}
          errors={errors}
          onChange={change}
          onDayIncompleteChange={setDayIncomplete}
        />

        <Field data-invalid={errors.category_id ? true : undefined}>
          <FieldLabel htmlFor={categoryFieldId}>Categoría (opcional)</FieldLabel>
          <NativeSelect
            id={categoryFieldId}
            className="w-full"
            value={categoryId}
            aria-invalid={errors.category_id ? true : undefined}
            aria-describedby={errors.category_id ? `${categoryFieldId}-error` : undefined}
            onChange={(event) => {
              setCategoryId(event.target.value)
              clear('category_id')
            }}
          >
            <NativeSelectOption value="">Según tus reglas</NativeSelectOption>
            {options.map((option) => (
              <NativeSelectOption key={option.id} value={option.id}>
                {option.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          {errors.category_id && (
            <FieldError id={`${categoryFieldId}-error`}>{errors.category_id}</FieldError>
          )}
        </Field>

        <Field data-invalid={errors.note ? true : undefined}>
          <FieldLabel htmlFor={noteId}>Nota (opcional)</FieldLabel>
          <Textarea
            id={noteId}
            value={note}
            maxLength={MAX_NOTE}
            rows={2}
            placeholder="Para acordarte de qué fue"
            className="min-h-20 resize-none"
            aria-invalid={errors.note ? true : undefined}
            aria-describedby={errors.note ? `${noteId}-error` : undefined}
            onChange={(event) => {
              setNote(event.target.value)
              clear('note')
            }}
          />
          {errors.note && <FieldError id={`${noteId}-error`}>{errors.note}</FieldError>}
        </Field>

        {failure && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>No pudimos anotar la compra</AlertTitle>
            <AlertDescription>{failure}</AlertDescription>
          </Alert>
        )}
      </form>
    </ResponsiveDialogContent>
  )
}
