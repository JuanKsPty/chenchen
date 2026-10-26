/**
 * <RuleForm rule categories layout autoFocus? onSaved onCancel? />
 *
 *   rule        the rule being edited, or null for a new one
 *   categories  the category tree (useCategories().data?.categories)
 *   layout      'card'   fields and buttons stacked, for the desktop card on Reglas
 *               'sheet'  fields scroll, the full-width 48px "Guardar" sits in a
 *                        footer: the mobile bottom sheet
 *   autoFocus   focus the text field on mount (desktop, after "Editar")
 *   onSaved     after the server took it, with the saved rule; the form has already
 *               said so in a toast
 *   onCancel    shows "Cancelar" (editing on desktop)
 *
 * A rule: movements whose concept contains a text go to a category, or count as a
 * transfer between the owner's accounts, or both. The server wants the text (60
 * characters at most) and a category or the transfer mark; the form checks the
 * same before sending. "Aplicarla a lo que ya tienes" files what is already
 * imported, never touching a category chosen by hand. A rejected form puts focus on
 * the first field to fix; a failure with nothing to fix, back on the button.
 */
import { CircleAlertIcon } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { CategoryNode, CategoryRule } from '@/api/types'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOptGroup,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import { categoryNames } from '@/lib/categories'
import { useSaveRule } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { focusWhenReady } from './focus'
import { fieldProblem, problemMessage } from './problem'

const MAX_TEXT = 60

type RuleField = 'match_text' | 'category_id'

interface Option {
  id: string
  name: string
}

/** The categories as <optgroup>s: Gastos first (what rules are mostly for), then Entradas. */
function categoryGroups(tree: CategoryNode[] | undefined) {
  const names = categoryNames(tree)
  const spend: Option[] = []
  const income: Option[] = []
  for (const node of tree ?? []) {
    const group = node.kind === 'income' ? income : spend
    group.push({ id: node.id, name: names.get(node.id) ?? node.name })
    for (const child of node.children) {
      group.push({ id: child.id, name: names.get(child.id) ?? child.name })
    }
  }
  const byName = (a: Option, b: Option) => a.name.localeCompare(b.name, 'es')
  return [
    { label: 'Gastos', options: spend.sort(byName) },
    { label: 'Entradas', options: income.sort(byName) },
  ].filter((group) => group.options.length > 0)
}

export function RuleForm({
  rule,
  categories,
  layout,
  autoFocus = false,
  onSaved,
  onCancel,
}: {
  rule: CategoryRule | null
  categories: CategoryNode[] | undefined
  layout: 'card' | 'sheet'
  autoFocus?: boolean
  onSaved: (saved: CategoryRule) => void
  onCancel?: () => void
}) {
  const save = useSaveRule()
  const ids = useId()
  const textRef = useRef<HTMLInputElement>(null)
  const categoryRef = useRef<HTMLSelectElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const groups = useMemo(() => categoryGroups(categories), [categories])

  const [text, setText] = useState(rule?.match_text ?? '')
  const [categoryId, setCategoryId] = useState(rule?.category_id ?? '')
  const [transfer, setTransfer] = useState(rule?.transfer ?? false)
  const [applyToExisting, setApplyToExisting] = useState(true)
  const [errors, setErrors] = useState<Partial<Record<RuleField, string>>>({})
  const [failure, setFailure] = useState<string | null>(null)

  useEffect(() => {
    if (autoFocus) textRef.current?.focus()
  }, [autoFocus])

  function submit(event: React.FormEvent) {
    event.preventDefault()
    if (save.isPending) return
    const trimmed = text.trim()
    const found = {
      match_text: !trimmed
        ? 'Escribe el texto que buscamos en el concepto.'
        : // Characters as the server counts them (runes), not UTF-16 units.
          Array.from(trimmed).length > MAX_TEXT
          ? `Usa ${MAX_TEXT} caracteres o menos.`
          : undefined,
      category_id:
        !categoryId && !transfer
          ? 'Elige una categoría o márcala como transferencia.'
          : undefined,
    }
    setErrors(found)
    setFailure(null)
    if (found.match_text || found.category_id) {
      focusWhenReady(() => (found.match_text ? textRef.current : categoryRef.current))
      return
    }

    save.mutate(
      {
        id: rule?.id,
        input: {
          match_text: trimmed,
          category_id: categoryId || null,
          transfer,
          apply_to_existing: applyToExisting,
        },
      },
      {
        onSuccess: (saved) => {
          const applied = saved.applied ?? 0
          toast(
            applied > 0
              ? `Regla guardada. La aplicamos a ${applied} ${applied === 1 ? 'movimiento' : 'movimientos'}.`
              : 'Regla guardada.',
          )
          if (!rule) {
            setText('')
            setCategoryId('')
            setTransfer(false)
            setApplyToExisting(true)
          }
          onSaved(saved)
        },
        onError: (error) => {
          // A duplicate is about the text: say so under the field.
          const duplicate =
            error instanceof ApiError && error.code === 'rule_exists'
              ? error.message
              : undefined
          const textProblem = fieldProblem(error, 'match_text') ?? duplicate
          if (textProblem) {
            setErrors({ match_text: textProblem })
            focusWhenReady(() => textRef.current)
          } else {
            setFailure(problemMessage(error))
            focusWhenReady(() => submitRef.current)
          }
        },
      },
    )
  }

  const textId = `${ids}-texto`
  const categoryFieldId = `${ids}-categoria`
  const sheet = layout === 'sheet'
  const submitLabel = rule ? 'Guardar cambios' : 'Crear regla'

  return (
    <form
      onSubmit={submit}
      noValidate
      className={cn('flex flex-col', sheet ? 'min-h-0 flex-1' : 'gap-6')}
    >
      <div
        className={cn(
          'flex flex-col gap-5',
          sheet && 'min-h-0 overflow-y-auto px-4 pt-1 pb-2',
        )}
      >
        {failure && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>No pudimos guardar la regla</AlertTitle>
            <AlertDescription>{failure}</AlertDescription>
          </Alert>
        )}

        <Field data-invalid={errors.match_text ? true : undefined}>
          <FieldLabel htmlFor={textId}>Si el concepto contiene</FieldLabel>
          <Input
            ref={textRef}
            id={textId}
            value={text}
            placeholder="Ej. SUPER 99"
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            maxLength={MAX_TEXT + 20}
            aria-invalid={errors.match_text ? true : undefined}
            aria-describedby={`${textId}-ayuda`}
            onChange={(event) => {
              setText(event.target.value)
              if (errors.match_text) setErrors((e) => ({ ...e, match_text: undefined }))
            }}
          />
          {errors.match_text ? (
            <FieldError id={`${textId}-ayuda`}>{errors.match_text}</FieldError>
          ) : (
            <FieldDescription id={`${textId}-ayuda`}>
              Tal como sale en tu estado de cuenta. Da igual si es mayúscula o minúscula.
            </FieldDescription>
          )}
        </Field>

        <Field data-invalid={errors.category_id ? true : undefined}>
          <FieldLabel htmlFor={categoryFieldId}>Va a la categoría</FieldLabel>
          <NativeSelect
            ref={categoryRef}
            id={categoryFieldId}
            className="w-full"
            value={categoryId}
            aria-invalid={errors.category_id ? true : undefined}
            onChange={(event) => {
              setCategoryId(event.target.value)
              if (errors.category_id) setErrors((e) => ({ ...e, category_id: undefined }))
            }}
          >
            <NativeSelectOption value="">
              {transfer ? 'Ninguna' : 'Elige una categoría'}
            </NativeSelectOption>
            {groups.map((group) => (
              <NativeSelectOptGroup key={group.label} label={group.label}>
                {group.options.map((option) => (
                  <NativeSelectOption key={option.id} value={option.id}>
                    {option.name}
                  </NativeSelectOption>
                ))}
              </NativeSelectOptGroup>
            ))}
          </NativeSelect>
          {errors.category_id && <FieldError>{errors.category_id}</FieldError>}
        </Field>

        <div className="flex flex-col gap-1">
          <CheckRow
            checked={transfer}
            onChange={(next) => {
              setTransfer(next)
              if (next && errors.category_id)
                setErrors((e) => ({ ...e, category_id: undefined }))
            }}
            label="Es una transferencia entre tus cuentas"
            hint="No cuenta como gasto ni como entrada."
          />
          <CheckRow
            checked={applyToExisting}
            onChange={setApplyToExisting}
            label="Aplicarla también a lo que ya importaste"
            hint="Nunca cambia lo que categorizaste a mano."
          />
        </div>
      </div>

      <div
        className={cn(
          'flex gap-2',
          sheet ? 'flex-col px-4 pt-3 pb-4' : 'flex-row flex-wrap',
        )}
      >
        <Button
          ref={submitRef}
          type="submit"
          size={sheet ? 'lg' : 'default'}
          disabled={save.isPending}
          className={cn(sheet && 'w-full')}
        >
          {save.isPending && <Spinner />}
          {submitLabel}
        </Button>
        {onCancel && (
          <Button
            type="button"
            variant="secondary"
            onClick={onCancel}
            disabled={save.isPending}
          >
            Cancelar
          </Button>
        )}
      </div>
    </form>
  )
}

/** A checkbox in a label row at least 44px tall; the hint wraps under the label. */
function CheckRow({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  hint: string
}) {
  return (
    <label className="flex min-h-11 cursor-pointer items-start gap-3 py-2.5">
      <Checkbox
        checked={checked}
        onCheckedChange={(value) => onChange(value === true)}
        className="mt-px"
      />
      <span className="flex min-w-0 flex-col">
        <span className="text-body font-medium text-foreground">{label}</span>
        <span className="text-small font-medium text-muted-foreground">{hint}</span>
      </span>
    </label>
  )
}
