/**
 * <CategorizeSheet entry open onOpenChange />
 *
 * Frozen signature (F6):
 *   entry         the movement to categorize, or null while nothing is selected
 *   open          whether it is showing
 *   onOpenChange  called with false when it closes (the scrim, Escape, "Cerrar", a
 *                 swipe down, or after a save)
 *
 * Controlled, and it finds the account, the categories and the rules itself, so a
 * caller only holds which movement is selected:
 *
 *   const [selected, setSelected] = useState<Entry | null>(null)
 *   <CategorizeSheet entry={selected} open={selected !== null}
 *                    onOpenChange={(open) => !open && setSelected(null)} />
 *
 * It keeps showing the last movement while it closes, so the content does not empty
 * out during the exit animation.
 *
 * Spec bottomSheet: below lg a bottom sheet (grab handle, swipe down to close), from
 * lg a dialog. "Categorizar" in heading 22 over what the bank printed, which cannot
 * change: "SUPER 99 VIA ESPAÑA · −$84.37 · 24 oct · Tarjeta ···1111". Then
 * what it means, which can:
 *   - the categories of its direction (spending or income) as a radio group, 52px
 *     rows with their icons (44 in the dialog), "Sin categoría" last;
 *   - "Es una transferencia entre tus cuentas" (it then needs no category);
 *   - a note.
 * The footer stays put: "Hacer lo mismo con <DESCRIPCIÓN>", which saves a rule and
 * applies it to the movements already here, and "Guardar".
 *
 * After saving, a toast: "Guardado" with "Deshacer" (puts back what changed), or,
 * with the rule, "Listo: regla creada, N movimientos" with "Ver regla". A rule that
 * has already filed other movements cannot be taken back cleanly, so that toast has
 * no undo.
 *
 * A purchase the owner noted that no statement has printed yet (origin 'capture') is
 * what they wrote, not what the bank printed, so it can change. Under its facts it
 * says how it was noted ("Anotada con Atajos el 26 oct a las 12:41", "Anotada por el
 * asistente" or "Anotada a mano") and "Cuenta en tus gastos. Tu saldo cambia cuando
 * llegue tu estado." (on a card someone else pays, which has neither a statement nor
 * a balance of the owner's: "Cuenta en tus gastos, no en lo que debes."). It has no
 * transfer option (a purchase is spending) and two ghost buttons:
 *   - "Corregir" swaps in a form for its Monto, ¿Dónde? and Día, whose "Cancelar"
 *     comes back here;
 *   - "Descartar compra", for one that never happened: "Listo: compra descartada"
 *     with "Deshacer", which takes it back.
 *
 * A printed line that a noted purchase was linked to (`noted`) says so under its
 * facts: "La anotaste como «Café Unido»." ("…con Atajos como…" when the shortcut
 * noted it, "El asistente la anotó como…" when Claude did), with a ghost "No es esta
 * compra" for a wrong link. That unlinks them, so the purchase counts again on its
 * own until its real line arrives, and closes the sheet: "Listo: separamos la compra
 * de este movimiento." with "Deshacer", which links them again.
 */
import { CircleAlertIcon } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import {
  linkCapture,
  restoreCapture,
  updateEntry,
  type CapturePatch,
  type EntryPatch,
} from '@/api/endpoints'
import type { Account, CategoryNode, Entry, NotedPurchase } from '@/api/types'
import {
  PurchaseFields,
  purchaseAmount,
  purchaseProblems,
  purchaseToast,
  serverProblems,
  type PurchaseField,
  type PurchaseValues,
} from '@/components/account/AddPurchaseDialog'
import { Amount } from '@/components/kit'
import { focusWhenReady } from '@/components/settings/focus'
import { problemMessage } from '@/components/settings/problem'
import { DESKTOP_QUERY, useMediaQuery } from '@/components/shell/media'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Sheet, SheetContent } from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { accountWithNumber, PAID_BY_OTHER_NOTE, paidByOther } from '@/lib/account'
import { UNCATEGORIZED_ICON, UNCATEGORIZED_LABEL, categoryIcon } from '@/lib/categories'
import {
  entryKindLabel,
  formatCivilDate,
  formatCivilDayMonth,
  formatDayAndTime,
  todayCivil,
} from '@/lib/format'
import { useReturnFocus } from '@/lib/focus'
import { RULES_PATH } from '@/lib/nav'
import {
  useAccounts,
  useCategories,
  useDiscardCapture,
  useEditCapture,
  useMoneyInvalidation,
  useRules,
  useSaveRule,
  useUnlinkCapture,
  useUpdateEntry,
} from '@/lib/queries'
import { cn } from '@/lib/utils'
import { amountToCents, isClean, type Errors } from '@/lib/validate'
import { SheetHandle } from './PickerSheet'
import { useSwipeDown } from './useSwipeDown'

/** The server keeps a rule's text to 60 characters. */
const RULE_TEXT_LIMIT = 60

/** The radio's value for "Sin categoría" (a category id is never empty). */
const UNCATEGORIZED_VALUE = 'sin-categoria'

/** spec voice.errorGeneric */
const GENERIC_ERROR =
  'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'

export function CategorizeSheet({
  entry,
  open,
  onOpenChange,
}: {
  entry: Entry | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  // The movement on screen: the selected one, or the last one while closing.
  const [shown, setShown] = useState(entry)
  if (entry && entry !== shown) setShown(entry)
  const desktop = useMediaQuery(DESKTOP_QUERY)
  const visible = open && shown !== null
  const close = () => onOpenChange(false)
  const focusProps = useFocusBack()

  if (desktop) {
    return (
      <Dialog open={visible} onOpenChange={onOpenChange}>
        {shown && (
          <DialogContent
            className="flex max-h-[min(46rem,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-xl"
            {...focusProps}
          >
            <CategorizeContent
              key={shown.id}
              entry={shown}
              layout="dialog"
              onDone={close}
            />
          </DialogContent>
        )}
      </Dialog>
    )
  }

  return (
    <Sheet open={visible} onOpenChange={onOpenChange}>
      {shown && (
        <SheetContent side="bottom" className="gap-0" {...focusProps}>
          <CategorizeContent key={shown.id} entry={shown} layout="sheet" onDone={close} />
        </SheetContent>
      )}
    </Sheet>
  )
}

/** The movement rows in a list: each opens Categorizar. */
function rowsIn(list: HTMLElement): HTMLElement[] {
  return Array.from(list.querySelectorAll<HTMLElement>('button[aria-haspopup="dialog"]'))
}

/**
 * Where focus starts and where it goes back to.
 *
 * Radix would focus the checked radio, which scrolls a long list to "Sin categoría"
 * before anyone has read it, so the sheet focuses itself; Tab then reaches the radio
 * group first. On closing, focus goes back to the row that opened it. When saving
 * took that row out of the list (working through "Sin categoría", where a
 * categorized movement leaves), it goes to the row now in its place, rather than
 * dropping to the top of the page.
 */
function useFocusBack() {
  const returnFocus = useReturnFocus()
  const place = useRef<{ list: HTMLElement; index: number } | null>(null)

  return {
    onOpenAutoFocus: (event: Event) => {
      returnFocus.onOpenAutoFocus()
      const opener = document.activeElement
      const list = opener instanceof HTMLElement ? opener.closest('section') : null
      place.current =
        opener instanceof HTMLElement && list
          ? { list, index: rowsIn(list).indexOf(opener) }
          : null
      event.preventDefault()
      ;(event.currentTarget as HTMLElement | null)?.focus({ preventScroll: true })
    },
    onCloseAutoFocus: (event: Event) => {
      returnFocus.onCloseAutoFocus(event)
      const spot = place.current
      place.current = null
      if (event.defaultPrevented || !spot || spot.index < 0 || !spot.list.isConnected)
        return
      const rows = rowsIn(spot.list)
      const next = rows[Math.min(spot.index, rows.length - 1)]
      if (next) {
        event.preventDefault()
        next.focus()
      }
    },
  }
}

interface CategoryChoice {
  id: string
  name: string
  icon: ReturnType<typeof categoryIcon>
}

/** The categories that fit a movement's direction, children after their parent. */
function choicesFor(entry: Entry, tree: CategoryNode[]): CategoryChoice[] {
  const kind = entry.amount.cents > 0 && entry.kind !== 'refund' ? 'income' : 'expense'
  const all = tree.flatMap((node) => [
    { node, id: node.id, name: node.name },
    ...node.children.map((child) => ({
      node,
      id: child.id,
      name: `${node.name} › ${child.name}`,
    })),
  ])
  const fits = all.filter(
    (choice) => choice.node.kind === kind || choice.id === entry.category_id,
  )
  return fits.map(({ id, name }) => ({ id, name, icon: categoryIcon(name) }))
}

/**
 * Categorizar, or for a noted purchase its "Corregir" form in the same dialog or
 * sheet. Coming back from Corregir, focus goes back to its button.
 */
function CategorizeContent({
  entry,
  layout,
  onDone,
}: {
  entry: Entry
  layout: 'sheet' | 'dialog'
  onDone: () => void
}) {
  const [correcting, setCorrecting] = useState(false)
  const [cameBack, setCameBack] = useState(false)

  if (correcting) {
    return (
      <CorrectForm
        entry={entry}
        layout={layout}
        onCancel={() => {
          setCorrecting(false)
          setCameBack(true)
        }}
        onDone={onDone}
      />
    )
  }
  return (
    <CategorizeForm
      entry={entry}
      layout={layout}
      onDone={onDone}
      onCorrect={() => setCorrecting(true)}
      focusCorrect={cameBack}
    />
  )
}

function CategorizeForm({
  entry,
  layout,
  onDone,
  onCorrect,
  focusCorrect,
}: {
  entry: Entry
  layout: 'sheet' | 'dialog'
  onDone: () => void
  /** Opens "Corregir"; only a noted purchase has it. */
  onCorrect: () => void
  /** Focus "Corregir" on mount: the owner is back from it. */
  focusCorrect: boolean
}) {
  const sheet = layout === 'sheet'
  // A noted purchase still pending: what the owner wrote, not what the bank printed.
  const capture = entry.origin === 'capture'
  // On a printed line: the purchase the owner noted that was linked to it.
  const noted = capture ? undefined : entry.noted
  const accounts = useAccounts()
  const categories = useCategories()
  const rules = useRules()
  const update = useUpdateEntry()
  const saveRule = useSaveRule()
  const discard = useDiscardCapture()
  const unlink = useUnlinkCapture()
  const invalidate = useMoneyInvalidation()
  const navigate = useNavigate()
  const swipe = useSwipeDown(onDone)
  const groupLabel = useId()
  const correctRef = useRef<HTMLButtonElement>(null)

  const wasTransfer = entry.kind === 'transfer'
  const [category, setCategory] = useState(entry.category_id ?? '')
  const [transfer, setTransfer] = useState(wasTransfer)
  const [note, setNote] = useState(entry.note ?? '')
  const [withRule, setWithRule] = useState(false)
  const [saving, setSaving] = useState(false)
  const [discarding, setDiscarding] = useState(false)
  const [unlinking, setUnlinking] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const busy = saving || discarding || unlinking

  useEffect(() => {
    if (focusCorrect) correctRef.current?.focus()
  }, [focusCorrect])

  const account = accounts.data?.accounts.find((a) => a.id === entry.account_id)
  const choices = useMemo(
    () => choicesFor(entry, categories.data?.categories ?? []),
    [entry, categories.data],
  )

  const description = entry.description.trim()
  const ruleText = Array.from(description).slice(0, RULE_TEXT_LIMIT).join('').trim()
  // A rule needs something to do: a category, or the transfer mark.
  const canRule = ruleText !== '' && (transfer || category !== '')
  const ruleChecked = withRule && canRule

  const save = async () => {
    if (busy) return
    const patch: EntryPatch = {}
    const nextCategory = transfer ? '' : category
    if ((entry.category_id ?? '') !== nextCategory) patch.category_id = nextCategory || null
    if ((entry.note ?? '') !== note.trim()) patch.note = note.trim()
    if (wasTransfer !== transfer) patch.transfer = transfer

    const changed = Object.keys(patch).length > 0
    if (!changed && !ruleChecked) return onDone()

    setSaving(true)
    setError(null)
    try {
      if (changed) await update.mutateAsync({ id: entry.id, patch })

      if (ruleChecked) {
        const existing = rules.data?.rules.find(
          (rule) => rule.match_text.trim().toLowerCase() === ruleText.toLowerCase(),
        )
        const saved = await saveRule.mutateAsync({
          id: existing?.id,
          input: {
            match_text: ruleText,
            category_id: transfer ? null : category || null,
            transfer,
            priority: existing?.priority,
            apply_to_existing: true,
          },
        })
        // The rule leaves alone what was categorized by hand, this movement among
        // them now; it still counts as one of the movements the rule covers.
        const count = (saved.applied ?? 0) + 1
        const what = existing ? 'regla actualizada' : 'regla creada'
        toast(`Listo: ${what}, ${count} ${count === 1 ? 'movimiento' : 'movimientos'}`, {
          action: { label: 'Ver regla', onClick: () => navigate(RULES_PATH) },
        })
      } else {
        const undo: EntryPatch = {}
        if ('category_id' in patch) undo.category_id = entry.category_id ?? null
        if ('note' in patch) undo.note = entry.note ?? ''
        if ('transfer' in patch) undo.transfer = wasTransfer
        toast('Guardado', {
          action: {
            label: 'Deshacer',
            // The sheet is gone by now, so this goes straight to the server.
            onClick: () => {
              updateEntry(entry.id, undo).then(
                () => invalidate(),
                () => toast('No se pudo deshacer. Intenta otra vez.'),
              )
            },
          },
        })
      }
      onDone()
    } catch (failure) {
      setError(failure instanceof ApiError ? failure.message : GENERIC_ERROR)
    } finally {
      setSaving(false)
    }
  }

  // A purchase that never happened (a declined tap, one noted twice) stops counting.
  const discardPurchase = async () => {
    if (busy) return
    setDiscarding(true)
    setError(null)
    try {
      await discard.mutateAsync(entry.id)
      toast('Listo: compra descartada', {
        action: {
          label: 'Deshacer',
          // The sheet is gone by now, so this goes straight to the server.
          onClick: () => {
            restoreCapture(entry.id).then(
              () => invalidate(),
              () => toast('No se pudo deshacer. Intenta otra vez.'),
            )
          },
        },
      })
      onDone()
    } catch (failure) {
      setError(failure instanceof ApiError ? failure.message : GENERIC_ERROR)
    } finally {
      setDiscarding(false)
    }
  }

  // "No es esta compra": the line and the purchase part ways. The line may lose the
  // category the purchase gave it, so the sheet closes rather than show a stale one.
  // A mistap is costly (the purchase may count again next to its own line, and
  // nothing else here links them again), so the toast can link them back.
  const unlinkPurchase = async (purchase: NotedPurchase) => {
    if (busy) return
    setUnlinking(true)
    setError(null)
    try {
      await unlink.mutateAsync(purchase.capture_id)
      toast('Listo: separamos la compra de este movimiento.', {
        action: {
          label: 'Deshacer',
          // The sheet is gone by now, so this goes straight to the server.
          onClick: () => {
            linkCapture(purchase.capture_id, entry.id).then(
              () => invalidate(),
              () => toast('No se pudo deshacer. Intenta otra vez.'),
            )
          },
        },
      })
      onDone()
    } catch (failure) {
      setError(problemMessage(failure))
    } finally {
      setUnlinking(false)
    }
  }

  return (
    <>
      <div
        className={cn(
          'relative flex shrink-0 flex-col gap-1',
          sheet ? 'touch-none px-4 pt-5 pb-2' : 'px-6 pt-6 pb-3',
        )}
        {...(sheet ? swipe : {})}
      >
        {sheet && <SheetHandle />}
        <DialogTitle className="pr-12 font-display text-heading font-bold">
          Categorizar
        </DialogTitle>
        <DialogDescription className="pr-10 text-small font-medium text-pretty break-words text-muted-foreground">
          <Facts entry={entry} account={account} />
          {capture && (
            <>
              <span className="block pt-1">{notedHow(entry)}</span>
              <span className="block">
                {account && paidByOther(account)
                  ? PAID_BY_OTHER_NOTE
                  : 'Cuenta en tus gastos. Tu saldo cambia cuando llegue tu estado.'}
              </span>
            </>
          )}
          {noted && <span className="block pt-1">{notedAs(noted)}</span>}
        </DialogDescription>
        {capture && (
          <div className="-ml-3 flex flex-wrap gap-1 pt-1">
            <Button ref={correctRef} variant="ghost" onClick={onCorrect}>
              Corregir
            </Button>
            <Button
              variant="ghost"
              onClick={() => void discardPurchase()}
              aria-disabled={discarding || undefined}
            >
              {discarding && <Spinner />}
              Descartar compra
            </Button>
          </div>
        )}
        {noted && (
          <div className="-ml-3 flex flex-wrap gap-1 pt-1">
            <Button
              variant="ghost"
              onClick={() => void unlinkPurchase(noted)}
              aria-disabled={unlinking || undefined}
            >
              {unlinking && <Spinner />}
              No es esta compra
            </Button>
          </div>
        )}
      </div>

      <div
        className={cn(
          'flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto',
          sheet ? 'px-2 pt-1 pb-2' : 'px-4 pb-2',
        )}
      >
        <div className="flex flex-col">
          <p id={groupLabel} className="sr-only">
            Categoría
          </p>
          <RadioGroup
            aria-labelledby={groupLabel}
            value={transfer ? '' : category || UNCATEGORIZED_VALUE}
            onValueChange={(value) =>
              setCategory(value === UNCATEGORIZED_VALUE ? '' : value)
            }
            disabled={transfer}
            className={cn('gap-0', !sheet && 'grid-cols-2 gap-x-2')}
          >
            {choices.map((choice) => (
              <ChoiceRow
                key={choice.id}
                value={choice.id}
                icon={choice.icon}
                sheet={sheet}
                disabled={transfer}
              >
                {choice.name}
              </ChoiceRow>
            ))}
            <ChoiceRow
              value={UNCATEGORIZED_VALUE}
              icon={UNCATEGORIZED_ICON}
              sheet={sheet}
              disabled={transfer}
            >
              {UNCATEGORIZED_LABEL}
            </ChoiceRow>
          </RadioGroup>
        </div>

        <div className="flex flex-col gap-4 px-3">
          {/* A noted purchase is spending, never money moving between accounts. */}
          {!capture && (
            <label className="flex min-h-11 cursor-pointer items-center gap-3 rounded-full py-1">
              <Checkbox
                checked={transfer}
                onCheckedChange={(checked) => setTransfer(checked === true)}
              />
              <span className="flex flex-col">
                <span className="text-body font-medium">
                  Es una transferencia entre tus cuentas
                </span>
                <span className="text-small font-medium text-muted-foreground">
                  No cuenta como gasto ni como entrada, como el pago de la tarjeta.
                </span>
              </span>
            </label>
          )}

          <NoteField value={note} onChange={setNote} />
        </div>
      </div>

      <div
        className={cn(
          'flex shrink-0 flex-col gap-3',
          sheet ? 'px-4 pt-3 pb-4' : 'px-6 pt-3 pb-6',
        )}
      >
        <label
          className={cn(
            'flex min-h-11 items-center gap-3 py-1',
            canRule ? 'cursor-pointer' : 'cursor-not-allowed',
          )}
        >
          <Checkbox
            checked={ruleChecked}
            disabled={!canRule}
            onCheckedChange={(checked) => setWithRule(checked === true)}
          />
          <span className="flex min-w-0 flex-col">
            <span
              className={cn(
                'text-body font-medium break-words',
                !canRule && 'text-muted-foreground',
              )}
            >
              Hacer lo mismo con {ruleText || entryKindLabel(entry.kind)}
            </span>
            <span className="text-small font-medium text-muted-foreground">
              {canRule
                ? 'Crea una regla y la aplica a los que ya tienes.'
                : 'Elige una categoría para crear la regla.'}
            </span>
          </span>
        </label>

        {error && (
          <Alert variant="attention" role="alert">
            <CircleAlertIcon aria-hidden="true" />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <div className={cn('flex gap-2', !sheet && 'justify-end')}>
          {!sheet && (
            <Button variant="secondary" onClick={onDone}>
              Cancelar
            </Button>
          )}
          <Button
            size={sheet ? 'lg' : 'default'}
            className={cn(sheet && 'w-full')}
            onClick={() => void save()}
            // Not disabled: that would drop the focus while it saves, and an error
            // then leaves it nowhere.
            aria-disabled={saving || undefined}
          >
            {saving && <Spinner />}
            Guardar
          </Button>
        </div>
      </div>
    </>
  )
}

function ChoiceRow({
  value,
  icon: Icon,
  sheet,
  disabled,
  children,
}: {
  value: string
  icon: ReturnType<typeof categoryIcon>
  sheet: boolean
  disabled: boolean
  children: ReactNode
}) {
  return (
    <label
      className={cn(
        'flex items-center gap-3 rounded-full px-3 transition-colors duration-180 ease-out',
        sheet ? 'h-13' : 'h-11',
        disabled
          ? 'cursor-not-allowed text-muted-foreground'
          : 'cursor-pointer hover:bg-muted',
      )}
    >
      <RadioGroupItem value={value} />
      <Icon aria-hidden="true" className="size-5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate text-body font-medium">{children}</span>
    </label>
  )
}

/**
 * How a noted purchase was noted: "Anotada con Atajos el 26 oct a las 12:41", "Anotada
 * por el asistente" (Claude, from what it read or was told) or "Anotada a mano".
 */
function notedHow(entry: Entry): string {
  switch (entry.capture_source) {
    case 'shortcut':
      return entry.booked_at
        ? `Anotada con Atajos el ${formatDayAndTime(entry.booked_at)}`
        : 'Anotada con Atajos'
    case 'agent':
      return 'Anotada por el asistente'
    default:
      return 'Anotada a mano'
  }
}

/**
 * What the purchase linked to a printed line was called: "La anotaste como «Café
 * Unido».", noted by the shortcut "La anotaste con Atajos como «Café Unido».", and
 * noted by the assistant "El asistente la anotó como «Café Unido».".
 */
function notedAs(noted: NotedPurchase): string {
  switch (noted.source) {
    case 'shortcut':
      return `La anotaste con Atajos como «${noted.merchant}».`
    case 'agent':
      return `El asistente la anotó como «${noted.merchant}».`
    default:
      return `La anotaste como «${noted.merchant}».`
  }
}

/** An amount as the owner would type it: "12.50" for −$12.50, built from the cents. */
function typedAmount(cents: number): string {
  const whole = Math.abs(cents)
  return `${Math.floor(whole / 100)}.${String(whole % 100).padStart(2, '0')}`
}

/**
 * "Corregir": a noted purchase's Monto, ¿Dónde? and Día, in Categorizar's place. Only
 * what changed is sent. The server works out again whether it counts and links it if
 * its line is now plain, so the toast is the same as when it was noted. "Cancelar"
 * goes back to Categorizar; a swipe down, the scrim, Escape or "Cerrar" close both.
 */
function CorrectForm({
  entry,
  layout,
  onCancel,
  onDone,
}: {
  entry: Entry
  layout: 'sheet' | 'dialog'
  onCancel: () => void
  onDone: () => void
}) {
  const sheet = layout === 'sheet'
  const swipe = useSwipeDown(onDone)
  const edit = useEditCapture()
  const formId = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const submitRef = useRef<HTMLButtonElement>(null)

  const [values, setValues] = useState<PurchaseValues>(() => ({
    amount: typedAmount(entry.amount.cents),
    merchant: entry.description,
    day: entry.booked_on,
  }))
  const [dayIncomplete, setDayIncomplete] = useState(false)
  const [errors, setErrors] = useState<Errors<PurchaseField>>({})
  const [failure, setFailure] = useState<string | null>(null)

  const change = (field: PurchaseField, value: string) => {
    setValues((current) => ({ ...current, [field]: value }))
    if (errors[field]) setErrors((current) => ({ ...current, [field]: undefined }))
  }
  const focusFirstProblem = () =>
    focusWhenReady(() =>
      formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]'),
    )

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (edit.isPending) return
    const found = purchaseProblems(values, dayIncomplete)
    setErrors(found)
    setFailure(null)
    if (!isClean(found)) {
      focusFirstProblem()
      return
    }
    const patch: CapturePatch = {}
    const amount = purchaseAmount(values.amount)
    if (amountToCents(amount) !== -entry.amount.cents) patch.amount = amount
    const merchant = values.merchant.trim()
    if (merchant !== entry.description) patch.merchant = merchant
    if (values.day !== entry.booked_on) patch.day = values.day
    if (Object.keys(patch).length === 0) return onCancel()

    edit.mutate(
      { id: entry.id, patch },
      {
        onSuccess: (capture) => {
          purchaseToast(capture, 'Listo: compra corregida')
          onDone()
        },
        onError: (failed) => {
          const fields = serverProblems(failed, ['amount', 'merchant', 'day'] as const)
          if (fields) {
            setErrors(fields)
            focusFirstProblem()
          } else {
            setFailure(problemMessage(failed))
            focusWhenReady(() => submitRef.current)
          }
        },
      },
    )
  }

  return (
    <>
      <div
        className={cn(
          'relative flex shrink-0 flex-col gap-1',
          sheet ? 'touch-none px-4 pt-5 pb-2' : 'px-6 pt-6 pb-3',
        )}
        {...(sheet ? swipe : {})}
      >
        {sheet && <SheetHandle />}
        <DialogTitle className="pr-12 font-display text-heading font-bold">
          Corregir compra
        </DialogTitle>
        <DialogDescription className="pr-10 text-small font-medium text-pretty text-muted-foreground">
          Corrige el monto, dónde fue o el día.
        </DialogDescription>
      </div>

      <form
        ref={formRef}
        id={formId}
        onSubmit={submit}
        noValidate
        className={cn(
          'flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto py-2',
          sheet ? 'px-4' : 'px-6',
        )}
      >
        <PurchaseFields
          values={values}
          errors={errors}
          onChange={change}
          onDayIncompleteChange={setDayIncomplete}
          autoFocus
        />
      </form>

      <div
        className={cn(
          'flex shrink-0 flex-col gap-3',
          sheet ? 'px-4 pt-3 pb-4' : 'px-6 pt-3 pb-6',
        )}
      >
        {failure && (
          <Alert variant="attention">
            <CircleAlertIcon aria-hidden="true" />
            <AlertTitle>No pudimos corregir la compra</AlertTitle>
            <AlertDescription>{failure}</AlertDescription>
          </Alert>
        )}
        <div className={cn('flex gap-2', sheet ? 'flex-col-reverse' : 'justify-end')}>
          <Button
            type="button"
            variant={sheet ? 'ghost' : 'secondary'}
            size={sheet ? 'lg' : 'default'}
            className={cn(sheet && 'w-full')}
            onClick={onCancel}
          >
            Cancelar
          </Button>
          <Button
            ref={submitRef}
            type="submit"
            form={formId}
            size={sheet ? 'lg' : 'default'}
            className={cn(sheet && 'w-full')}
            // Not disabled, for the same reason as Categorizar's "Guardar".
            aria-disabled={edit.isPending || undefined}
          >
            {edit.isPending && <Spinner />}
            Guardar
          </Button>
        </div>
      </div>
    </>
  )
}

/** What the bank printed: description · amount · day · account. */
function Facts({ entry, account }: { entry: Entry; account?: Account }) {
  return (
    <>
      {entry.description || entryKindLabel(entry.kind)}
      {' · '}
      <Amount
        amount={entry.amount}
        size="inline"
        signed
        tone="auto"
        className="whitespace-nowrap"
      />
      {' · '}
      <span className="whitespace-nowrap">
        {/* The year only when it isn't this one, as the day headings do. */}
        {entry.booked_on.slice(0, 4) === todayCivil().slice(0, 4)
          ? formatCivilDayMonth(entry.booked_on)
          : formatCivilDate(entry.booked_on)}
      </span>
      {account && (
        <>
          {' · '}
          <span className="whitespace-nowrap">{accountWithNumber(account)}</span>
        </>
      )}
    </>
  )
}

function NoteField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const id = useId()
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-small font-semibold">
        Nota
      </label>
      <Textarea
        id={id}
        value={value}
        maxLength={500}
        rows={2}
        placeholder="Para acordarte de qué fue"
        className="min-h-20 resize-none"
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}
