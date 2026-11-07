/**
 * <IncomeDialog plan open onOpenChange returnTo? />
 *
 *   returnTo  where focus goes on closing when what opened the dialog is gone: the
 *             paycheck, when "Editar ingreso" in its sheet opened it
 *
 * "Ingreso esperado": one row per paycheck of the month on screen (two of $650 are
 * two rows, so each can say "entró" or "por entrar" on its own day). Each row has
 * ¿Qué es?, Monto, Día que llega and, optional, Cómo aparece en tu estado: the text
 * its deposit carries, which is how the plan sees that it arrived. "Agregar otro
 * ingreso" adds a row; "Quitar" takes one out. A live line under them adds them up.
 *
 * "Solo para noviembre" makes every change hold for this month alone (a bonus, a
 * month with one paycheck); otherwise they hold from this month on, which is what a
 * plan normally wants. Next month on screen: "Vale desde diciembre. Noviembre no
 * cambia."
 *
 * Opened by "Editar ingreso" (the first time, PlanSetupDialog sets the income up with
 * the rest); with no paycheck planned yet it starts with one empty row. Below lg a
 * bottom sheet, from lg a dialog, with
 * "Guardar" as its one primary. Each opening picks the ids of the rows it adds and
 * sends them on every try, so a retry never makes a paycheck twice. Only what
 * changed is sent, one row at a time; when the server refuses one, the rows already
 * saved are taken back, so nothing is left half saved, and the refused row is marked
 * field by field, with focus on the first one. The toast says what is free now
 * ("Guardado: $580.00 libres en noviembre.") and its "Deshacer" sends back what each
 * paycheck was.
 */
import { PlusIcon } from 'lucide-react'
import { useId, useRef, useState, type RefObject } from 'react'
import { toast } from 'sonner'

import { deletePlanItem, savePlanItem, type PlanItemInput } from '@/api/endpoints'
import type { MonthPlan, PlanIncomeLine, PlanScope } from '@/api/types'
import { serverProblems } from '@/components/account/AddPurchaseDialog'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { Amount } from '@/components/kit'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { FieldLegend, FieldSet } from '@/components/ui/field'
import { money } from '@/lib/format'
import { amountProblem, amountToCents, isClean, type Errors } from '@/lib/validate'
import {
  CheckRow,
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
  SubmitButton,
  TextField,
  unlessGone,
  useOpenRound,
  useReturnFocusOr,
  type ItemField,
} from './forms'
import { usePlanInvalidation, usePlanUndo } from './usePlan'
import { monthWord, scopeNote } from './words'

interface Row {
  /** The item's id: its own for a paycheck already planned, a new one per opening. */
  id: string
  existing?: PlanIncomeLine
  name: string
  amount: string
  day: string
  match: string
}

type RowErrors = Errors<ItemField>

/** One write and what puts it back. */
interface Step {
  row?: string
  send: () => Promise<unknown>
  undo: () => Promise<unknown>
}

const blankRow = (): Row => ({
  id: crypto.randomUUID(),
  name: '',
  amount: '',
  day: '',
  match: '',
})

const rowOf = (line: PlanIncomeLine): Row => ({
  id: line.id,
  existing: line,
  name: line.name,
  amount: line.amount.formatted,
  day: line.day ? String(line.day) : '',
  match: line.match_text,
})

export function IncomeDialog({
  plan,
  open,
  onOpenChange,
  returnTo,
}: {
  plan: MonthPlan
  open: boolean
  onOpenChange: (open: boolean) => void
  returnTo?: RefObject<HTMLElement | null>
}) {
  const returnFocus = useReturnFocusOr(returnTo)
  const round = useOpenRound(open)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <IncomeForm
        key={round}
        plan={plan}
        returnFocus={returnFocus}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

function IncomeForm({
  plan,
  returnFocus,
  onDone,
}: {
  plan: MonthPlan
  returnFocus: ReturnType<typeof useReturnFocusOr>
  onDone: () => void
}) {
  const formId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const invalidate = usePlanInvalidation()
  const undo = usePlanUndo()
  const month = plan.month
  const first = plan.income.items.length === 0

  const [rows, setRows] = useState<Row[]>(() =>
    first ? [blankRow()] : plan.income.items.map(rowOf),
  )
  const [removed, setRemoved] = useState<PlanIncomeLine[]>([])
  const [only, setOnly] = useState(false)
  const [errors, setErrors] = useState<Record<string, RowErrors>>({})
  const [failure, setFailure] = useState<string | null>(null)
  const [sending, setSending] = useState(false)

  const change = (
    id: string,
    field: 'name' | 'amount' | 'day' | 'match',
    value: string,
  ) => {
    setRows((current) => current.map((r) => (r.id === id ? { ...r, [field]: value } : r)))
    const key = field === 'match' ? 'match_text' : field
    if (errors[id]?.[key]) {
      setErrors((current) => ({ ...current, [id]: { ...current[id], [key]: undefined } }))
    }
  }

  const remove = (row: Row) => {
    setRows((current) => current.filter((r) => r.id !== row.id))
    const line = row.existing
    if (line) setRemoved((current) => [...current, line])
  }

  // What the rows add up to, from the amounts that read as amounts.
  const total = rows.reduce((sum, row) => {
    const text = planAmount(row.amount)
    return amountProblem(text) ? sum : sum + (amountToCents(text) ?? 0)
  }, 0)

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (sending) return
    const found: Record<string, RowErrors> = {}
    for (const row of rows) {
      const problems: RowErrors = {
        name: nameProblem(row.name),
        amount: amountProblem(planAmount(row.amount)),
        day: dayProblem(row.day, 'Elige el día que llega.'),
      }
      if (!isClean(problems)) found[row.id] = problems
    }
    setErrors(found)
    setFailure(null)
    if (Object.keys(found).length > 0) {
      focusFirstProblem(formRef)
      return
    }

    const chosen: PlanScope = only ? 'only' : 'from'
    const steps: Step[] = []
    for (const row of rows) {
      const scope = scopeFor(row.existing, month, chosen)
      const input: PlanItemInput = {
        kind: 'income',
        month,
        scope,
        name: row.name.trim(),
        amount: planAmount(row.amount),
        day: Number(row.day),
        match_text: row.match.trim(),
        category_id: row.existing?.category_id ?? null,
      }
      const before = row.existing
      if (before && unchanged(before, input)) continue
      steps.push({
        row: row.id,
        send: () => savePlanItem(row.id, input),
        undo: before
          ? () => savePlanItem(row.id, itemInput(before, month, scope))
          : () => deletePlanItem(row.id, month, scope),
      })
    }
    for (const line of removed) {
      const scope = scopeFor(line, month, chosen)
      steps.push({
        // Gone already (an earlier try took it out and lost the answer) is done.
        send: () => deletePlanItem(line.id, month, scope).catch(unlessGone),
        undo: () => savePlanItem(line.id, itemInput(line, month, scope)),
      })
    }
    if (steps.length === 0) return onDone()

    setSending(true)
    const done: Step[] = []
    for (const step of steps) {
      try {
        await step.send()
        done.push(step)
      } catch (error) {
        // All or nothing: what went through goes back first, so the plan is as it
        // was while the form says what is wrong, and "Cancelar" leaves nothing
        // half saved that no toast could undo. A retry sends it all again, with the
        // same ids.
        await undoAll(done)
        void invalidate()
        setSending(false)
        const fields = serverProblems(error, ITEM_FIELDS)
        if (fields && step.row) {
          setErrors({ [step.row]: fields })
          focusFirstProblem(formRef)
        } else {
          setFailure(problemMessage(error))
          focusWhenReady(() => submitRef.current)
        }
        return
      }
    }
    void invalidate()
    setSending(false)

    // What the month on screen has free now: only its income changed.
    const free = plan.summary.free.cents + (total - plan.income.expected.cents)
    const said = free > 0 ? `${money(free)} libres en ${monthWord(month)}.` : null
    toast(
      first
        ? `Listo: ${said ?? `tu ingreso de ${monthWord(month)}.`}`
        : `Guardado: ${said ?? `tu ingreso de ${monthWord(month)}.`}`,
      {
        action: {
          label: 'Deshacer',
          onClick: () =>
            undo(async () => {
              for (const step of [...done].reverse()) await step.undo()
            }),
        },
      },
    )
    onDone()
  }

  return (
    <ResponsiveDialogContent
      {...returnFocus}
      title="Ingreso esperado"
      description={
        scopeNote(plan) ?? 'Una fila por cada pago que te llega, como cada quincena.'
      }
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost" size="lg" className="lg:h-11">
              Cancelar
            </Button>
          </DialogClose>
          <SubmitButton form={formId} pending={sending} buttonRef={submitRef}>
            Guardar
          </SubmitButton>
        </>
      }
    >
      <form
        ref={formRef}
        id={formId}
        onSubmit={submit}
        noValidate
        className="flex flex-col gap-6"
      >
        {rows.map((row, index) => (
          <IncomeRow
            key={row.id}
            row={row}
            number={index + 1}
            errors={errors[row.id] ?? {}}
            autoFocus={first && index === 0}
            onChange={(field, value) => change(row.id, field, value)}
            onRemove={() => remove(row)}
          />
        ))}

        <Button
          type="button"
          variant="ghost"
          className="-ml-3 self-start"
          onClick={() => setRows((current) => [...current, blankRow()])}
        >
          <PlusIcon aria-hidden="true" />
          {rows.length === 0 ? 'Agregar ingreso' : 'Agregar otro ingreso'}
        </Button>

        <p aria-live="polite" className="text-body font-semibold">
          Suman <Amount amount={total} size="inline" /> al mes.
        </p>

        <CheckRow
          checked={only}
          onChange={setOnly}
          label={`Solo para ${monthWord(month)}`}
          hint="Los demás meses quedan como estaban."
        />

        {failure && <FormFailure title="No pudimos guardar tu ingreso" message={failure} />}
      </form>
    </ResponsiveDialogContent>
  )
}

/**
 * The months a row's change holds for. A paycheck planned for this month alone (an
 * extra added with "Sumar a lo libre") stays that way: from this month on would
 * make a one-off come back every month.
 */
function scopeFor(line: PlanIncomeLine | undefined, month: string, chosen: PlanScope) {
  return line && line.from === month && line.to === month ? 'only' : chosen
}

/**
 * Takes back, last first, the writes that went through before one was refused. One
 * that cannot be taken back stays; the plan refreshes after, so it shows.
 */
async function undoAll(done: Step[]) {
  for (const step of [...done].reverse()) {
    try {
      await step.undo()
    } catch {
      // Left as it is: the refreshed plan says so.
    }
  }
}

/** Whether a row still says what its paycheck already was: nothing to send. */
function unchanged(line: PlanIncomeLine, input: PlanItemInput): boolean {
  return (
    line.name === input.name &&
    line.amount.cents === amountToCents(input.amount) &&
    line.day === input.day &&
    line.match_text === input.match_text
  )
}

function IncomeRow({
  row,
  number,
  errors,
  autoFocus,
  onChange,
  onRemove,
}: {
  row: Row
  number: number
  errors: RowErrors
  autoFocus: boolean
  onChange: (field: 'name' | 'amount' | 'day' | 'match', value: string) => void
  onRemove: () => void
}) {
  const legend = row.name.trim() || `Ingreso ${number}`
  return (
    <FieldSet className="gap-3">
      <div className="flex min-h-11 items-center justify-between gap-3">
        <FieldLegend variant="label" className="mb-0 truncate">
          {legend}
        </FieldLegend>
        <Button
          type="button"
          variant="ghost"
          className="-mr-3"
          aria-label={`Quitar ${legend}`}
          onClick={onRemove}
        >
          Quitar
        </Button>
      </div>
      <TextField
        label="¿Qué es?"
        value={row.name}
        onChange={(value) => onChange('name', value)}
        error={errors.name}
        placeholder="Ej. Sueldo"
        maxLength={MAX_NAME}
        autoFocus={autoFocus}
      />
      <div className="grid grid-cols-2 items-start gap-3">
        <MoneyField
          label="Monto"
          value={row.amount}
          onChange={(value) => onChange('amount', value)}
          error={errors.amount}
        />
        <DayField
          label="Día que llega"
          value={row.day}
          onChange={(value) => onChange('day', value)}
          error={errors.day}
        />
      </div>
      <TextField
        label="Cómo aparece en tu estado (opcional)"
        value={row.match}
        onChange={(value) => onChange('match', value)}
        error={errors.match_text}
        description="Así sabemos cuándo ya entró."
        maxLength={MAX_MATCH}
      />
    </FieldSet>
  )
}
