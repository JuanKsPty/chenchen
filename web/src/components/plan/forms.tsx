/**
 * What the plan's dialogs share, in AddPurchaseDialog's pattern.
 *
 *   useOpenRound(open)      a key that changes each time a dialog opens: its form
 *                           remounts blank (and picks its ids) once per opening,
 *                           counted on opening so what was typed stays while it closes
 *   TextField · MoneyField · DayField · CategoryField
 *                           a labelled field with its hint and, when it has one, its
 *                           problem under it (aria-invalid, so a form can focus the
 *                           first one)
 *   ScopeField              "Desde este mes" / "Solo este mes": which months an edit
 *                           or a removal holds for
 *   CheckRow                a checkbox in a label row at least 44px tall
 *   RemoveButton            the ghost text button that takes an item out, in the body
 *                           and never in the footer: the toast undoes it, so it never
 *                           asks first (nothing here moves money)
 *   SubmitButton            the dialog's one primary, last in its footer; aria-disabled
 *                           while it sends, so focus is not lost
 *   FormFailure             the Alert for a refusal that is not about one field: a real
 *                           error, the one thing in the plan drawn in the error colour
 *   useReturnFocusOr(fallback?)
 *                           useReturnFocus, falling back to `fallback` when what had
 *                           focus left with a sheet
 *   nameProblem · dayProblem · planAmount · centsText · itemInput · focusFirstProblem
 *
 * Every amount the plan keeps is more than zero (an income, a payment, a contribution
 * or a cap of $0 is said by taking it out), so amountProblem's refusal of zero is
 * right for all of them.
 */
import { CircleAlertIcon } from 'lucide-react'
import { useId, useMemo, useState, type ReactNode, type RefObject } from 'react'

import type { PlanItemInput } from '@/api/endpoints'
import type { PlanFixedLine, PlanIncomeLine, PlanSavingLine, PlanScope } from '@/api/types'
import { purchaseAmount } from '@/components/account/AddPurchaseDialog'
import { focusWhenReady } from '@/components/settings/focus'
import { monthName } from '@/components/spending/period'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import type { CategoryOption } from '@/lib/categories'
import { useReturnFocus } from '@/lib/focus'

/** The server's limits on a name and on the text a movement is recognised by. */
export const MAX_NAME = 60
export const MAX_MATCH = 60

/** The fields an item's form shows, by the names the server reports problems under. */
export const ITEM_FIELDS = [
  'name',
  'amount',
  'day',
  'match_text',
  'category_id',
  'account_id',
  'scope',
] as const
export type ItemField = (typeof ITEM_FIELDS)[number]

export function useOpenRound(open: boolean): number {
  const [round, setRound] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setRound((n) => n + 1)
  }
  return round
}

/** The amount as it is sent: as typed, without "$" or spaces (AddPurchaseDialog). */
export const planAmount = purchaseAmount

/** "650.00" for 65,000 cents, from the integer: the text an amount travels as. */
export function centsText(cents: number): string {
  const whole = Math.trunc(cents / 100)
  const rest = String(Math.abs(cents % 100)).padStart(2, '0')
  return `${whole}.${rest}`
}

/**
 * What the server refuses in a name: a control or format character that is not a
 * space (a zero-width joiner pasted along with it, say), which cannot be shown or
 * could pass for something else. Said here too, so a form of several rows never
 * gets halfway before the server refuses one.
 */
const INVISIBLE = /[\p{Cf}\u0000-\u0008\u000e-\u001f\u007f-\u0084\u0086-\u009f]/u

export function nameProblem(text: string): string | undefined {
  const name = text.trim()
  if (!name) return 'Escribe qué es.'
  if (INVISIBLE.test(name)) return 'El nombre tiene caracteres que no se pueden mostrar.'
  if ([...name].length > MAX_NAME) return `Usa ${MAX_NAME} caracteres o menos.`
  return undefined
}

export function dayProblem(text: string, missing: string): string | undefined {
  const day = Number(text)
  return Number.isInteger(day) && day >= 1 && day <= 31 ? undefined : missing
}

/**
 * What an item was for `month`, as a form would send it again: an undo, or the
 * values a dialog does not show (a saving's goal) carried along unchanged.
 */
export function itemInput(
  item: PlanIncomeLine | PlanFixedLine | PlanSavingLine,
  month: string,
  scope: PlanScope,
): PlanItemInput {
  return {
    kind: item.kind,
    month,
    scope,
    name: item.name,
    amount: item.amount.formatted,
    day: item.day,
    match_text: item.match_text,
    category_id: item.category_id ?? null,
    account_id: item.account_id ?? null,
    target: item.target?.formatted,
    target_month: item.target_month ?? undefined,
  }
}

/**
 * useReturnFocus, with somewhere to go when what had focus is gone. A dialog opened
 * from a sheet's own button ("Editar pago fijo") remembers that button, which leaves
 * with the sheet; `fallback`, the row that opened the sheet, takes focus instead,
 * rather than Radix's default, <body>, for a dialog with no trigger.
 */
export function useReturnFocusOr(fallback?: RefObject<HTMLElement | null>) {
  const returnFocus = useReturnFocus()
  return useMemo(
    () => ({
      onOpenAutoFocus: returnFocus.onOpenAutoFocus,
      onCloseAutoFocus: (event: Event) => {
        returnFocus.onCloseAutoFocus(event)
        const element = fallback?.current
        if (!event.defaultPrevented && element?.isConnected) {
          event.preventDefault()
          element.focus()
        }
      },
    }),
    [returnFocus, fallback],
  )
}

/** Focus on the first field with a problem, once it is marked. */
export function focusFirstProblem(form: RefObject<HTMLElement | null>) {
  focusWhenReady(() => form.current?.querySelector<HTMLElement>('[aria-invalid="true"]'))
}

function describedBy(...ids: (string | false | undefined)[]): string | undefined {
  const found = ids.filter(Boolean)
  return found.length ? found.join(' ') : undefined
}

export function TextField({
  label,
  value,
  onChange,
  error,
  description,
  placeholder,
  maxLength,
  autoFocus,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  error?: string
  description?: ReactNode
  placeholder?: string
  maxLength?: number
  autoFocus?: boolean
}) {
  const id = useId()
  return (
    <Field data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        value={value}
        maxLength={maxLength}
        autoComplete="off"
        autoCapitalize="sentences"
        placeholder={placeholder}
        autoFocus={autoFocus}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(
          description ? `${id}-hint` : undefined,
          error && `${id}-error`,
        )}
        onChange={(event) => onChange(event.target.value)}
      />
      {description && <FieldDescription id={`${id}-hint`}>{description}</FieldDescription>}
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </Field>
  )
}

export function MoneyField({
  label,
  value,
  onChange,
  error,
  description,
  autoFocus,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  error?: string
  description?: ReactNode
  autoFocus?: boolean
}) {
  const id = useId()
  return (
    <Field data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        value={value}
        inputMode="decimal"
        autoComplete="off"
        placeholder="0.00"
        autoFocus={autoFocus}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(
          description ? `${id}-hint` : undefined,
          error && `${id}-error`,
        )}
        onChange={(event) => onChange(event.target.value)}
      />
      {description && (
        // Live: a hint worked out from what was chosen (the category's months
        // before) changes while the dialog is open.
        <FieldDescription id={`${id}-hint`} aria-live="polite">
          {description}
        </FieldDescription>
      )}
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </Field>
  )
}

const DAYS = Array.from({ length: 31 }, (_, i) => String(i + 1))

/** A day of the month, 1 to 31, as a native select: no typing a 32nd. */
export function DayField({
  label,
  value,
  onChange,
  error,
  description,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  error?: string
  description?: ReactNode
}) {
  const id = useId()
  return (
    <Field data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <NativeSelect
        id={id}
        className="w-full"
        value={value}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(
          description ? `${id}-hint` : undefined,
          error && `${id}-error`,
        )}
        onChange={(event) => onChange(event.target.value)}
      >
        <NativeSelectOption value="">Elige el día</NativeSelectOption>
        {DAYS.map((day) => (
          <NativeSelectOption key={day} value={day}>
            {`Día ${day}`}
          </NativeSelectOption>
        ))}
      </NativeSelect>
      {description && <FieldDescription id={`${id}-hint`}>{description}</FieldDescription>}
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </Field>
  )
}

/** A category, or none: `none` names the empty choice ("Sin categoría"). */
export function CategoryField({
  label,
  value,
  onChange,
  options,
  none,
  error,
  description,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  options: CategoryOption[]
  none: string
  error?: string
  description?: ReactNode
}) {
  const id = useId()
  return (
    <Field data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <NativeSelect
        id={id}
        className="w-full"
        value={value}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(
          description ? `${id}-hint` : undefined,
          error && `${id}-error`,
        )}
        onChange={(event) => onChange(event.target.value)}
      >
        <NativeSelectOption value="">{none}</NativeSelectOption>
        {options.map((option) => (
          <NativeSelectOption key={option.id} value={option.id}>
            {option.name}
          </NativeSelectOption>
        ))}
      </NativeSelect>
      {description && <FieldDescription id={`${id}-hint`}>{description}</FieldDescription>}
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </Field>
  )
}

/**
 * Which months an edit or a removal holds for. From the month on screen on, the
 * default: the plan carries forward. Or that month alone: the months around it keep
 * what they had.
 */
export function ScopeField({
  month,
  value,
  onChange,
}: {
  month: string
  value: PlanScope
  onChange: (value: PlanScope) => void
}) {
  const legendId = useId()
  return (
    <FieldSet className="gap-1">
      <FieldLegend id={legendId} variant="label">
        ¿Para qué meses?
      </FieldLegend>
      <RadioGroup
        value={value}
        onValueChange={(next) => onChange(next === 'only' ? 'only' : 'from')}
        aria-labelledby={legendId}
      >
        <ScopeChoice
          value="from"
          label="Desde este mes"
          hint={`${monthName(month)} y los que siguen.`}
        />
        <ScopeChoice
          value="only"
          label="Solo este mes"
          hint="Los demás meses quedan como estaban."
        />
      </RadioGroup>
    </FieldSet>
  )
}

function ScopeChoice({
  value,
  label,
  hint,
}: {
  value: PlanScope
  label: string
  hint: string
}) {
  return (
    <label className="-mx-3 flex min-h-13 cursor-pointer items-center gap-3 rounded-2xl px-3 py-2 hover:bg-muted">
      <RadioGroupItem value={value} />
      <span className="flex min-w-0 flex-col">
        <span className="text-body font-medium text-foreground">{label}</span>
        <span className="text-small font-medium text-muted-foreground">{hint}</span>
      </span>
    </label>
  )
}

/** A checkbox in a label row at least 44px tall; the hint wraps under the label. */
export function CheckRow({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  hint?: string
}) {
  return (
    <label className="flex min-h-11 cursor-pointer items-start gap-3 py-2.5">
      <Checkbox
        checked={checked}
        onCheckedChange={(next) => onChange(next === true)}
        className="mt-px"
      />
      <span className="flex min-w-0 flex-col">
        <span className="text-body font-medium text-foreground">{label}</span>
        {hint && (
          <span className="text-small font-medium text-muted-foreground">{hint}</span>
        )}
      </span>
    </label>
  )
}

export function RemoveButton({
  onClick,
  pending,
  children,
}: {
  onClick: () => void
  pending: boolean
  children: ReactNode
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      className="-ml-3 self-start"
      aria-disabled={pending || undefined}
      onClick={() => {
        if (!pending) onClick()
      }}
    >
      {pending && <Spinner aria-hidden="true" />}
      {children}
    </Button>
  )
}

export function SubmitButton({
  form,
  pending,
  buttonRef,
  children,
}: {
  form: string
  pending: boolean
  buttonRef?: RefObject<HTMLButtonElement | null>
  children: ReactNode
}) {
  return (
    <Button
      ref={buttonRef}
      type="submit"
      form={form}
      size="lg"
      className="lg:h-11"
      // Not disabled: that would drop the focus while it saves, and an error then
      // leaves it nowhere.
      aria-disabled={pending || undefined}
    >
      {pending && <Spinner aria-hidden="true" />}
      {children}
    </Button>
  )
}

export function FormFailure({ title, message }: { title: string; message: string }) {
  return (
    <Alert variant="attention">
      <CircleAlertIcon aria-hidden="true" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  )
}
