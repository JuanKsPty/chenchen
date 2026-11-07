/**
 * <PlanSetupDialog plan suggestions failed open onOpenChange returnTo? />
 *
 *   suggestions  what the history suggests planning; undefined while it loads
 *   failed       it could not load: the setup starts from rows to type
 *   returnTo     where focus goes on closing when what opened the setup is gone: the
 *                plan's heading, once the plan it set up is on the page
 *
 * "Armar mi plan", the first time (and ?armar=1, Gastos' "Armar plan"): the month's
 * plan in three short steps, from what chenchén found in the owner's history, about
 * five taps when it found it all. Each step has its own primary, "Siguiente" and at
 * the end "Guardar plan", and every step but the first goes back with "Atrás".
 *   1. "¿Cuánto te entra al mes?" The paychecks it found, checked and filled in under
 *      "Vimos $650.00 dos veces al mes, cerca del 16 y del 30.": each one's amount and
 *      day can change, and its box leaves it out. "Agregar otro ingreso" adds a row to
 *      type. With no history, a row to type.
 *   2. "Tus pagos fijos": what went out every month, with its amount and "cerca del
 *      día 18", checked when it went out in three months or more (one seen in two may
 *      be something else). "Agregar otro pago" adds a row to type. A card is never
 *      one, and the step says so: what it bought already counts.
 *   3. "¿Cuánto quieres ahorrar?", optional: what reached each savings or brokerage
 *      account every month, checked, its amount open to change; otherwise, or as
 *      well, an amount and the account it goes to.
 * Under every step, live, what that leaves free: "$1,061.02 libres al mes.", or,
 * when payments and savings come to more than the income, by how much. Never a
 * figure below zero.
 *
 * "Guardar plan" saves each item from the month on screen on, a suggestion with the
 * text its statement prints and the category its lines are filed under, so the plan
 * recognises the next one. A suggestion left unchecked is only left out: it is
 * offered again under its card, never turned down from here. Each opening picks the
 * items' ids and sends them on every try, so a retry never plans anything twice. All
 * or nothing, like IncomeDialog: when the server refuses one, what was saved is taken
 * back, and the step with the refused row opens with its fields marked, focus on the
 * first one; any other refusal is an Alert above the buttons. Then the plan is
 * refreshed before the dialog closes, so focus can land on it, and the toast says
 * "Listo: tu plan de noviembre está armado.", its "Deshacer" taking out all it made.
 *
 * Below lg a bottom sheet, from lg a dialog. The step's question is the dialog's
 * title, "Paso 1 de 3" under it, and focus goes to it on each step. It opens on the
 * first step's first field (a row to type, or the first box), or, while the history
 * still loads, on its question over a frame with no figures; the steps then fill
 * that same dialog, built once from the history as it first arrived, so a refetch
 * while it is open never changes the rows or what is said about them.
 */
import { PlusIcon } from 'lucide-react'
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type ReactNode,
  type RefObject,
  type SetStateAction,
} from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import { deletePlanItem, type PlanItemInput } from '@/api/endpoints'
import type { MonthPlan, PlanSuggestion, PlanSuggestions } from '@/api/types'
import { serverProblems } from '@/components/account/AddPurchaseDialog'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { Amount } from '@/components/kit'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { FieldLegend, FieldSet } from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'
import { shiftMonth } from '@/lib/format'
import { useAccounts } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { amountProblem, amountToCents, isClean, type Errors } from '@/lib/validate'
import {
  CheckRow,
  DayField,
  dayProblem,
  focusFirstProblem,
  FormFailure,
  MAX_NAME,
  MoneyField,
  nameProblem,
  planAmount,
  SavingAccountField,
  savingAccounts,
  SubmitButton,
  TextField,
  unlessGone,
  useOpenRound,
  useReturnFocusOr,
} from './forms'
import { saveSuggested, suggestedItem } from './Suggestions'
import { usePlanInvalidation, usePlanUndo } from './usePlan'
import { freeLine, monthWord, seenIncome, suggestionHint } from './words'

type Step = 1 | 2 | 3

const TITLES: Record<Step, string> = {
  1: '¿Cuánto te entra al mes?',
  2: 'Tus pagos fijos',
  3: '¿Cuánto quieres ahorrar?',
}

/**
 * A payment seen in this many months comes checked: one seen in fewer may have been
 * something else twice.
 */
const SURE_MONTHS = 3

const NO_HISTORY: PlanSuggestions = { income: [], fixed: [], savings: [] }

/** The fields a row shows, by the names the server reports problems under. */
type Field = 'name' | 'amount' | 'day' | 'account_id'
type RowErrors = Errors<Field>

interface Row {
  /** The item's id, picked once per opening and sent on every try. */
  id: string
  /** What the history suggested, for a row it filled in; none for one to type. */
  suggestion?: PlanSuggestion
  /** Whether it goes into the plan: a suggestion's box can leave it out. */
  on: boolean
  name: string
  amount: string
  day: string
}

/**
 * What the steps were built from: the history as it first arrived in this opening,
 * and whether it could not load. The rows are made from it once, and so is what the
 * steps say about them.
 */
interface Seed {
  history: PlanSuggestions
  failed: boolean
}

/** The saving typed in the last step: optional, with the account it goes to. */
interface OwnSaving {
  id: string
  shown: boolean
  amount: string
  account: string
}

/** One item to save, and where its row is: its step, and the fields it shows there. */
interface Write {
  id: string
  step: Step
  label: string
  fields: readonly Field[]
  input: PlanItemInput
}

const typedRow = (): Row => ({
  id: crypto.randomUUID(),
  on: true,
  name: '',
  amount: '',
  day: '',
})

const suggestedRow = (s: PlanSuggestion, on: boolean): Row => ({
  id: crypto.randomUUID(),
  suggestion: s,
  on,
  name: s.name,
  amount: s.amount.formatted,
  day: s.day ? String(s.day) : '',
})

/** What a typed amount comes to, or nothing while it does not read as one. */
function centsOf(text: string): number {
  const amount = planAmount(text)
  return amountProblem(amount) ? 0 : (amountToCents(amount) ?? 0)
}

const total = (rows: Row[]) =>
  rows.reduce((sum, row) => (row.on ? sum + centsOf(row.amount) : sum), 0)

/** The step's question, the dialog's title: where focus goes on each step. */
const questionOf = (form: HTMLElement | null) =>
  form?.closest('[role="dialog"]')?.querySelector<HTMLElement>('[data-slot="dialog-title"]')

/**
 * The first step's first field: the first box the history checked, or what a row
 * to type is (the box's hidden input, which a form gets, is not a field).
 */
const firstFieldOf = (form: HTMLElement | null) =>
  form?.querySelector<HTMLElement>('[role="checkbox"], input:not([type="checkbox"])')

export function PlanSetupDialog({
  plan,
  suggestions,
  failed,
  open,
  onOpenChange,
  returnTo,
}: {
  plan: MonthPlan
  suggestions: PlanSuggestions | undefined
  failed: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  returnTo?: RefObject<HTMLElement | null>
}) {
  const returnFocus = useReturnFocusOr(returnTo)
  const round = useOpenRound(open)
  const history = suggestions ?? (failed ? NO_HISTORY : undefined)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <SetupForm
        key={round}
        plan={plan}
        history={history}
        failed={failed && !suggestions}
        returnFocus={returnFocus}
        onDone={() => onOpenChange(false)}
      />
    </Dialog>
  )
}

function SetupForm({
  plan,
  history,
  failed,
  returnFocus,
  onDone,
}: {
  plan: MonthPlan
  /** Undefined while it loads. */
  history: PlanSuggestions | undefined
  failed: boolean
  returnFocus: ReturnType<typeof useReturnFocusOr>
  onDone: () => void
}) {
  const formId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)
  const addRef = useRef<HTMLButtonElement>(null)
  const invalidate = usePlanInvalidation()
  const undo = usePlanUndo()
  const accounts = useAccounts()
  const month = plan.month

  const [step, setStep] = useState<Step>(1)
  const [seed, setSeed] = useState<Seed | null>(null)
  const [income, setIncome] = useState<Row[]>([])
  const [fixed, setFixed] = useState<Row[]>([])
  const [savings, setSavings] = useState<Row[]>([])
  const [own, setOwn] = useState<OwnSaving>(() => ({
    id: crypto.randomUUID(),
    shown: false,
    amount: '',
    account: '',
  }))
  // The rows are made once, from the history as it first arrives: on opening, or
  // while the first step's frame waits for it. Never again in this opening.
  if (history && !seed) {
    setSeed({ history, failed })
    setIncome(
      history.income.length > 0
        ? history.income.map((s) => suggestedRow(s, true))
        : [typedRow()],
    )
    setFixed(history.fixed.map((s) => suggestedRow(s, s.months_seen >= SURE_MONTHS)))
    setSavings(history.savings.map((s) => suggestedRow(s, true)))
    setOwn((current) => ({ ...current, shown: history.savings.length === 0 }))
  }
  // Whether this opening waited for the history: its steps then arrive after the
  // dialog opened on their question.
  const [waited] = useState(() => !history)
  // The row whose first field takes focus when it appears: the one just added.
  // Moving to another step forgets it, so coming back focuses the step's question.
  const [added, setAdded] = useState<string | null>(null)
  const [errors, setErrors] = useState<Record<string, RowErrors>>({})
  const [failure, setFailure] = useState<string | null>(null)
  const [sending, setSending] = useState(false)

  // The accounts a typed saving can go to: not one a suggestion already sets aside
  // in, since an account backs one saving at a time.
  const choices = useMemo(() => {
    const suggested = new Set(seed?.history.savings.map((s) => s.account_id))
    return savingAccounts(accounts.data?.accounts).filter((a) => !suggested.has(a.id))
  }, [accounts.data, seed])

  // Radix would open on the first button, a row's "Quitar" when there is a row to
  // type: the first step opens on its first field instead, as AddPurchaseDialog does
  // on Monto, or on its question while the history loads. Done here rather than with
  // autoFocus, which would skip onOpenAutoFocus: that is where useReturnFocus learns
  // what opened the setup, for focus to go back to it on closing.
  const focus = useMemo(
    () => ({
      onOpenAutoFocus: (event: Event) => {
        returnFocus.onOpenAutoFocus()
        const target = firstFieldOf(formRef.current) ?? questionOf(formRef.current)
        if (target) {
          event.preventDefault()
          target.focus()
        }
      },
      onCloseAutoFocus: returnFocus.onCloseAutoFocus,
    }),
    [returnFocus],
  )

  // Steps that arrive after the dialog opened take focus from their question, as
  // they would have on opening; never from wherever the owner has since moved it.
  useEffect(() => {
    if (!waited || !seed) return
    const question = questionOf(formRef.current)
    if (!question || document.activeElement !== question) return
    focusWhenReady(() => firstFieldOf(formRef.current))
  }, [waited, seed])

  const incomeTotal = total(income)
  const free =
    incomeTotal - total(fixed) - total(savings) - (own.shown ? centsOf(own.amount) : 0)

  const clear = (id: string, field?: Field) => {
    if (!errors[id]) return
    setErrors((current) => ({
      ...current,
      [id]: field ? { ...current[id], [field]: undefined } : {},
    }))
  }

  /** A row's field changed, or its box: what was wrong with it no longer is. */
  const edit =
    (set: Dispatch<SetStateAction<Row[]>>) =>
    (id: string, field: 'name' | 'amount' | 'day', value: string) => {
      set((rows) => rows.map((r) => (r.id === id ? { ...r, [field]: value } : r)))
      clear(id, field)
    }
  const toggle = (set: Dispatch<SetStateAction<Row[]>>) => (id: string, on: boolean) => {
    set((rows) => rows.map((r) => (r.id === id ? { ...r, on } : r)))
    if (!on) clear(id)
  }
  const add = (set: Dispatch<SetStateAction<Row[]>>) => () => {
    const row = typedRow()
    setAdded(row.id)
    set((rows) => [...rows, row])
  }
  const remove = (set: Dispatch<SetStateAction<Row[]>>) => (id: string) => {
    set((rows) => rows.filter((r) => r.id !== id))
    clear(id)
    // Its "Quitar" leaves with it: focus goes to the button that adds one.
    focusWhenReady(() => addRef.current)
  }
  const editOwn = (field: 'amount' | 'account', value: string) => {
    setOwn((current) => ({ ...current, [field]: value }))
    clear(own.id, field === 'account' ? 'account_id' : 'amount')
  }

  /** What is wrong with the rows of `at`, by row. */
  const problemsOf = (at: Step): Record<string, RowErrors> => {
    const rows = at === 1 ? income : at === 2 ? fixed : savings
    const found: Record<string, RowErrors> = {}
    for (const row of rows) {
      if (!row.on) continue
      const problems: RowErrors = {
        name: row.suggestion ? undefined : nameProblem(row.name),
        amount: amountProblem(planAmount(row.amount)),
        day:
          at === 3
            ? undefined
            : dayProblem(
                row.day,
                at === 1 ? 'Elige el día que llega.' : 'Elige el día que sale.',
              ),
      }
      if (!isClean(problems)) found[row.id] = problems
    }
    // Optional, unless an account says a saving was meant.
    if (at === 3 && own.shown && (own.amount.trim() || own.account)) {
      const amount = amountProblem(planAmount(own.amount))
      if (amount) found[own.id] = { amount }
    }
    return found
  }

  /** To another step: its question takes focus, from the top. */
  const go = (next: Step) => {
    setStep(next)
    setAdded(null)
    setErrors({})
    setFailure(null)
    formRef.current?.parentElement?.scrollTo({ top: 0 })
    focusWhenReady(() => questionOf(formRef.current))
  }

  /** Every item the three steps say to plan, in their order. */
  const writes = (): Write[] => {
    const of = (row: Row, at: Step, kind: 'income' | 'fixed' | 'saving'): Write => {
      const s = row.suggestion
      if (s) {
        return {
          id: row.id,
          step: at,
          label: s.name,
          // A payment's row shows nothing to change; a saving has no day.
          fields:
            kind === 'fixed' ? [] : kind === 'income' ? ['amount', 'day'] : ['amount'],
          input: {
            ...suggestedItem(s, month),
            amount: planAmount(row.amount),
            ...(kind === 'saving' ? {} : { day: Number(row.day) }),
          },
        }
      }
      return {
        id: row.id,
        step: at,
        label: row.name.trim(),
        fields: ['name', 'amount', 'day'],
        input: {
          kind,
          month,
          scope: 'from',
          name: row.name.trim(),
          amount: planAmount(row.amount),
          day: Number(row.day),
          match_text: '',
          category_id: null,
        },
      }
    }
    const list = [
      ...income.filter((r) => r.on).map((r) => of(r, 1, 'income')),
      ...fixed.filter((r) => r.on).map((r) => of(r, 2, 'fixed')),
      ...savings.filter((r) => r.on).map((r) => of(r, 3, 'saving')),
    ]
    if (own.shown && own.amount.trim()) {
      // Named by the account the owner calls it by, as a suggested one is; never by
      // the bank's name for it.
      const alias = accounts.data?.accounts.find((a) => a.id === own.account)?.alias?.trim()
      const name = alias ? [...`Ahorro en ${alias}`].slice(0, MAX_NAME).join('') : null
      list.push({
        id: own.id,
        step: 3,
        label: name ?? 'Tu ahorro',
        fields: ['amount', 'account_id'],
        input: {
          kind: 'saving',
          month,
          scope: 'from',
          name: name ?? 'Ahorro de cada mes',
          amount: planAmount(own.amount),
          account_id: own.account || null,
          match_text: '',
          category_id: null,
        },
      })
    }
    return list
  }

  const save = async () => {
    const list = writes()
    if (list.length === 0) return onDone()
    setSending(true)
    const done: Write[] = []
    for (const write of list) {
      try {
        await saveSuggested(write.id, write.input)
        done.push(write)
      } catch (error) {
        // All or nothing: what went through goes back first, so "Cancelar" never
        // leaves half a plan that no toast could undo. So does the one that failed,
        // which may have been saved with its answer lost (one never saved is gone
        // already). A retry sends it all again, with the same ids.
        await takeOut([...done, write], month, { quietly: true })
        void invalidate()
        setSending(false)
        let fields = serverProblems(error, write.fields)
        // One account, one saving: said where the account is chosen.
        if (
          !fields &&
          write.fields.includes('account_id') &&
          error instanceof ApiError &&
          error.code === 'account_has_goal'
        ) {
          fields = { account_id: error.message }
        }
        if (fields) {
          setStep(write.step)
          setErrors({ [write.id]: fields })
          focusFirstProblem(formRef)
        } else {
          setFailure(failureText(error, write.label))
          focusWhenReady(() => submitRef.current)
        }
        return
      }
    }
    // The page shows the plan before the dialog leaves it, so focus has a place.
    await invalidate()
    setSending(false)
    toast(`Listo: tu plan de ${monthWord(month)} está armado.`, {
      action: { label: 'Deshacer', onClick: () => undo(() => takeOut(done, month)) },
    })
    onDone()
  }

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (sending || !seed) return
    const problems = problemsOf(step)
    setErrors(problems)
    setFailure(null)
    if (Object.keys(problems).length > 0) {
      focusFirstProblem(formRef)
      return
    }
    if (step < 3) return go((step + 1) as Step)
    await save()
  }

  const rowsOf = (
    rows: Row[],
    set: Dispatch<SetStateAction<Row[]>>,
    {
      noun,
      placeholder,
      dayLabel,
    }: { noun: string; placeholder: string; dayLabel?: string },
    hint: (s: PlanSuggestion) => string,
    editable: boolean,
  ) =>
    rows.map((row, index) =>
      row.suggestion ? (
        <SuggestedRow
          key={row.id}
          row={row}
          suggestion={row.suggestion}
          hint={hint(row.suggestion)}
          editable={editable}
          dayLabel={dayLabel}
          errors={errors[row.id] ?? {}}
          onToggle={(on) => toggle(set)(row.id, on)}
          onChange={(field, value) => edit(set)(row.id, field, value)}
        />
      ) : (
        <TypedRow
          key={row.id}
          row={row}
          legend={row.name.trim() || `${noun} ${index + 1}`}
          placeholder={placeholder}
          dayLabel={dayLabel ?? 'Día'}
          errors={errors[row.id] ?? {}}
          autoFocus={row.id === added}
          onChange={(field, value) => edit(set)(row.id, field, value)}
          onRemove={() => remove(set)(row.id)}
        />
      ),
    )

  let body: ReactNode
  if (!seed) {
    // While the history loads: the first step's frame, never figures.
    body = (
      <>
        <p className="text-body text-pretty text-muted-foreground">
          Buscando lo que se repite en tus cuentas…
        </p>
        <div className="flex flex-col gap-3" aria-hidden="true">
          <Skeleton className="h-11" />
          <Skeleton className="h-11" />
        </div>
      </>
    )
  } else if (step === 1) {
    body = (
      <>
        <p className="text-body text-pretty text-muted-foreground">
          {seenIncome(seed.history.income) ??
            (seed.failed
              ? 'No pudimos ver tus movimientos. Escribe lo que te entra en un mes normal.'
              : 'Lo que te entra en un mes normal: una fila por cada pago, como cada quincena.')}
        </p>
        {rowsOf(
          income,
          setIncome,
          { noun: 'Ingreso', placeholder: 'Ej. Sueldo', dayLabel: 'Día que llega' },
          (s) => suggestionHint(s, { amount: false }),
          true,
        )}
        <AddButton buttonRef={addRef} onClick={add(setIncome)}>
          {income.length === 0 ? 'Agregar ingreso' : 'Agregar otro ingreso'}
        </AddButton>
      </>
    )
  } else if (step === 2) {
    body = (
      <>
        <p className="text-body text-pretty text-muted-foreground">
          {seed.history.fixed.length > 0
            ? 'Salen solos cada mes. Marca los que sí son.'
            : seed.failed
              ? 'Agrega lo que sale solo cada mes: alquiler, luz, suscripciones.'
              : 'No vimos pagos que se repitan. Agrega los tuyos, como el alquiler o la luz.'}
        </p>
        {rowsOf(
          fixed,
          setFixed,
          { noun: 'Pago', placeholder: 'Ej. Alquiler', dayLabel: 'Día que sale' },
          (s) => suggestionHint(s, { amount: false, months: true }),
          false,
        )}
        <AddButton buttonRef={addRef} onClick={add(setFixed)}>
          {fixed.length === 0 ? 'Agregar un pago' : 'Agregar otro pago'}
        </AddButton>
        <p className="text-small font-medium text-pretty text-muted-foreground">
          Tu tarjeta no va aquí: sus compras ya cuentan.
        </p>
      </>
    )
  } else {
    // The months a suggested saving's amount is the average of: the complete months
    // before this one, by the server's clock.
    const now = plan.today.slice(0, 7)
    const span = `de ${monthWord(shiftMonth(now, -3))} a ${monthWord(shiftMonth(now, -1))}`
    body = (
      <>
        <p className="text-body text-pretty text-muted-foreground">
          {seed.history.savings.length > 0
            ? 'Lo que ya pasas a tus cuentas de ahorro cada mes. Cámbialo si quieres.'
            : 'Si quieres, aparta algo cada mes, aunque sea poquito. Sale de lo libre.'}
        </p>
        {rowsOf(
          savings,
          setSavings,
          { noun: 'Ahorro', placeholder: '' },
          () => `Lo que pasaste en promedio ${span}.`,
          true,
        )}
        {own.shown ? (
          <div className="flex flex-col gap-4">
            <MoneyField
              label={
                seed.history.savings.length > 0
                  ? 'Otro ahorro al mes (opcional)'
                  : 'Ahorro al mes (opcional)'
              }
              value={own.amount}
              onChange={(value) => editOwn('amount', value)}
              error={errors[own.id]?.amount}
              autoFocus={own.id === added}
            />
            <SavingAccountField
              value={own.account}
              onChange={(value) => editOwn('account', value)}
              choices={choices}
              error={errors[own.id]?.account_id}
            />
          </div>
        ) : (
          <AddButton
            buttonRef={addRef}
            onClick={() => {
              setAdded(own.id)
              setOwn((current) => ({ ...current, shown: true }))
            }}
          >
            Agregar otro ahorro
          </AddButton>
        )}
      </>
    )
  }

  const next = plan.state === 'future' ? ` · Vale desde ${monthWord(month)}.` : ''

  return (
    <ResponsiveDialogContent
      {...focus}
      title={TITLES[step]}
      description={`Paso ${step} de 3${next}`}
      footer={
        <>
          {step === 1 ? (
            <DialogClose asChild>
              <Button variant="ghost" size="lg" className="lg:h-11">
                Cancelar
              </Button>
            </DialogClose>
          ) : (
            <Button
              type="button"
              variant="ghost"
              size="lg"
              className="lg:h-11"
              aria-disabled={sending || undefined}
              onClick={() => {
                if (!sending) go((step - 1) as Step)
              }}
            >
              Atrás
            </Button>
          )}
          <SubmitButton form={formId} pending={sending || !seed} buttonRef={submitRef}>
            {step === 3 ? 'Guardar plan' : 'Siguiente'}
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
        {body}

        {seed && (
          <p aria-live="polite" className="text-body font-semibold text-pretty">
            {freeLine(free, incomeTotal)}
          </p>
        )}

        {failure && <FormFailure title="No pudimos guardar tu plan" message={failure} />}
      </form>
    </ResponsiveDialogContent>
  )
}

/**
 * Takes out, last first, what the setup made, from the month on screen on: the
 * toast's "Deshacer", or, `quietly`, the writes that went through before one was
 * refused (one that cannot be taken back stays; the refreshed plan shows it).
 */
async function takeOut(done: Write[], month: string, { quietly = false } = {}) {
  for (const write of [...done].reverse()) {
    try {
      await deletePlanItem(write.id, month, 'from').catch(unlessGone)
    } catch (error) {
      if (!quietly) throw error
    }
  }
}

/**
 * A refusal about no field the setup shows (a suggested payment's row has none to
 * change), said with the row it was about.
 */
function failureText(error: unknown, label: string): string {
  const detail =
    error instanceof ApiError && error.fields ? Object.values(error.fields)[0] : undefined
  return detail ? `${label}: ${detail}` : problemMessage(error)
}

function AddButton({
  buttonRef,
  onClick,
  children,
}: {
  buttonRef: RefObject<HTMLButtonElement | null>
  onClick: () => void
  children: string
}) {
  return (
    <Button
      ref={buttonRef}
      type="button"
      variant="ghost"
      className="-ml-3 self-start"
      onClick={onClick}
    >
      <PlusIcon aria-hidden="true" />
      {children}
    </Button>
  )
}

/**
 * A row the history filled in: its box, named by the suggestion, and under it, when
 * `editable` and checked, its amount and (with `dayLabel`) its day. A payment's row
 * shows its amount beside the name instead: it is planned as it went out, and its
 * dialog changes it later.
 */
function SuggestedRow({
  row,
  suggestion: s,
  hint,
  editable,
  dayLabel,
  errors,
  onToggle,
  onChange,
}: {
  row: Row
  suggestion: PlanSuggestion
  hint: string
  editable: boolean
  dayLabel?: string
  errors: RowErrors
  onToggle: (on: boolean) => void
  onChange: (field: 'amount' | 'day', value: string) => void
}) {
  return (
    <div className="flex flex-col gap-1">
      <CheckRow
        checked={row.on}
        onChange={onToggle}
        label={s.name}
        hint={hint}
        aside={editable ? undefined : <Amount amount={s.amount} />}
      />
      {editable && row.on && (
        // Lined up with the name, under the box; named by it for a screen reader,
        // since every row's fields are "Monto" and "Día".
        <div
          role="group"
          aria-label={s.name}
          className={cn(
            'grid items-start gap-3 pb-2 pl-8.5',
            dayLabel ? 'grid-cols-2' : 'grid-cols-1',
          )}
        >
          <MoneyField
            label={dayLabel ? 'Monto' : 'Al mes'}
            value={row.amount}
            onChange={(value) => onChange('amount', value)}
            error={errors.amount}
          />
          {dayLabel && (
            <DayField
              label={dayLabel}
              value={row.day}
              onChange={(value) => onChange('day', value)}
              error={errors.day}
            />
          )}
        </div>
      )}
    </div>
  )
}

/** A row to type: ¿Qué es?, Monto and the day, with "Quitar". */
function TypedRow({
  row,
  legend,
  placeholder,
  dayLabel,
  errors,
  autoFocus,
  onChange,
  onRemove,
}: {
  row: Row
  legend: string
  placeholder: string
  dayLabel: string
  errors: RowErrors
  autoFocus: boolean
  onChange: (field: 'name' | 'amount' | 'day', value: string) => void
  onRemove: () => void
}) {
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
        placeholder={placeholder}
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
          label={dayLabel}
          value={row.day}
          onChange={(value) => onChange('day', value)}
          error={errors.day}
        />
      </div>
    </FieldSet>
  )
}
