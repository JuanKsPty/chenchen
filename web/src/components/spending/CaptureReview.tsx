/**
 * <CaptureReview keepWhenEmpty? className? />
 *
 *   keepWhenEmpty  show the section even with nothing to review (the page was opened
 *                  at #compras, or the last purchase was just answered): it then says
 *                  "No quedan compras por revisar."
 *
 * The section `#compras` (CAPTURES_HREF; Inicio's notice and an import's result link
 * here), the twin of TransferSuggestions: "Compras por revisar", the purchases the
 * owner noted that their statement does not confirm. One block each: what was noted
 * ("Café Unido", account and day, the amount), then what settles it.
 *   - printed: lines of the same amount could be it, and which one was not obvious.
 *     Per line, "¿Es este movimiento?" with the line, then "Sí, es este" (links them)
 *     and "No" (never offered for it again). Lines the owner cannot tell apart (same
 *     day, amount and text: two equal coffees) are asked about once, "2 iguales":
 *     "Sí" links one of them, "No" turns them all down.
 *   - missing: a statement went past its day without it. When a line comes close (a
 *     tip, a late charge), the closest one: "Tu estado trae −$46.00 en CAFE UNIDO el
 *     25 oct. ¿Es la misma compra?" with "Sí, es esta" and "No", which brings the next
 *     one. Then "Tu estado no la muestra. Si no se cobró, descártala." with
 *     "Descartar" (the toast has "Deshacer") and "Unir con otro": the account's
 *     expense lines around its day that no other purchase has, closest day first, and
 *     picking one links it, whatever its amount.
 *
 * Secondary and ghost buttons only, never a primary: the view keeps one (the bar's
 * "Importar estado"). One secondary per question, the likeliest answer; on the
 * purchase's surface2 block it takes the card colour, so it keeps its shape.
 *
 * Not tied to the month on screen. An answer takes its question away at once, and every
 * other question waits until the server has it and the list is fresh: one answer can
 * settle the rest (a "No" to one of two lines links the other), and the purchase that
 * moves into the answered one's place must not take a double-click's second click (a
 * "No" there could never be taken back). A toast says what happened, and focus moves
 * to what is next in the same purchase, else to the purchase now in its place, else
 * to the "none left" line. On failure the question comes back, focused, and the toast
 * says what failed. An answer stays hidden until the list no longer has it; from then
 * on the list decides, so a purchase that comes back to review (unlinked in another
 * tab) shows again. If the list cannot load, the section shows the error line when it
 * was asked for (#compras) and stays away otherwise; it never claims there is nothing
 * left.
 */
import { useQuery } from '@tanstack/react-query'
import {
  useEffect,
  useId,
  useRef,
  useState,
  type MouseEvent,
  type MouseEventHandler,
} from 'react'
import { toast } from 'sonner'

import { fetchEntries, restoreCapture, type EntryQuery } from '@/api/endpoints'
import type { Account, CaptureReviewItem, Entry } from '@/api/types'
import { ResponsiveDialogContent } from '@/components/accounts/ResponsiveDialog'
import { Amount, SectionCard } from '@/components/kit'
import { problemMessage } from '@/components/settings/problem'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose } from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { accountWithNumber } from '@/lib/account'
import { entryKindLabel, formatCivilDayMonth, shiftDay } from '@/lib/format'
import { useReturnFocus } from '@/lib/focus'
import { CAPTURES_HREF } from '@/lib/nav'
import {
  keys,
  useAccounts,
  useCaptureReview,
  useDiscardCapture,
  useLinkCapture,
  useMoneyInvalidation,
  useRejectCapture,
} from '@/lib/queries'
import { cn } from '@/lib/utils'
import { LoadError } from './LoadError'

export const CAPTURES_ID = CAPTURES_HREF.slice(CAPTURES_HREF.indexOf('#') + 1)

/**
 * The days around a purchase "Unir con otro" lists lines from. Wider than the
 * server's own suggestions (a day before to ten after, within 30% of the amount):
 * this is for the line nothing suggested.
 */
const PICK_DAYS_BEFORE = 3
const PICK_DAYS_AFTER = 14

type Answer =
  { kind: 'link'; line: Entry } | { kind: 'reject'; lines: Entry[] } | { kind: 'discard' }

type Click = MouseEventHandler<HTMLButtonElement>

/** What one purchase still asks, once the answers given here are taken out. */
interface Open {
  item: CaptureReviewItem
  candidates: Entry[]
  near: Entry[]
}

const pairKey = (captureId: string, lineId: string) => `${captureId}:${lineId}`

function toggled(set: ReadonlySet<string>, key: string, on: boolean): ReadonlySet<string> {
  const copy = new Set(set)
  if (on) copy.add(key)
  else copy.delete(key)
  return copy
}

/** The keys of `set` that are still in `live`: `set` itself when all of them are. */
function kept(set: ReadonlySet<string>, live: ReadonlySet<string>): ReadonlySet<string> {
  const copy = new Set([...set].filter((key) => live.has(key)))
  return copy.size === set.size ? set : copy
}

export function CaptureReview({
  keepWhenEmpty = false,
  className,
}: {
  keepWhenEmpty?: boolean
  className?: string
}) {
  const review = useCaptureReview()
  const accounts = useAccounts()
  const link = useLinkCapture()
  const reject = useRejectCapture()
  const discard = useDiscardCapture()
  const invalidate = useMoneyInvalidation()
  // Purchases linked or discarded here, and lines answered "No", hidden at once while
  // the list refreshes behind them; purchases with an answer on its way.
  const [settled, setSettled] = useState<ReadonlySet<string>>(() => new Set())
  const [refused, setRefused] = useState<ReadonlySet<string>>(() => new Set())
  const [busy, setBusy] = useState<ReadonlySet<string>>(() => new Set())
  const [answered, setAnswered] = useState(false)
  // "Unir con otro": the purchase it is for stays while the dialog closes.
  const [picking, setPicking] = useState<CaptureReviewItem | null>(null)
  const [pickerOpen, setPickerOpen] = useState(false)
  // Where focus goes once the list re-renders: the purchase answered (by id), or,
  // when it is gone, the one now at its index.
  const focusNext = useRef<{ key: string; index: number } | null>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const doneRef = useRef<HTMLParagraphElement>(null)

  // Once the list no longer has an answer, hiding it is the list's job: forgotten
  // here, so a purchase that comes back to review later shows again rather than
  // leaving "No quedan compras por revisar." next to Inicio's count.
  const [seen, setSeen] = useState(review.data)
  if (review.data !== seen) {
    setSeen(review.data)
    const items = review.data?.captures
    if (items) {
      const lines = items.flatMap((i) =>
        [...i.candidates, ...i.near].map((l) => pairKey(i.id, l.id)),
      )
      setSettled((s) => kept(s, new Set(items.map((i) => i.id))))
      setRefused((s) => kept(s, new Set(lines)))
    }
  }
  // While any answer is on its way, every other waits (see the header).
  const waiting = busy.size > 0

  const open: Open[] = []
  for (const item of review.data?.captures ?? []) {
    if (settled.has(item.id)) continue
    const left = (lines: Entry[]) =>
      lines.filter((l) => !refused.has(pairKey(item.id, l.id)))
    const candidates = left(item.candidates)
    // A printed purchase with no line left to ask about is changing state; the
    // refreshed list says what it is now.
    if (item.state === 'printed' && candidates.length === 0) continue
    open.push({ item, candidates, near: left(item.near) })
  }
  const byId = new Map((accounts.data?.accounts ?? []).map((a) => [a.id, a]))

  // After an answer the question is gone: give focus to something that still exists.
  // Not while "Unir con otro" is open; it closes first.
  useEffect(() => {
    const target = focusNext.current
    if (!target || pickerOpen) return
    focusNext.current = null
    const items = Array.from(listRef.current?.children ?? [])
    const item =
      items.find((li) => li.getAttribute('data-capture') === target.key) ??
      items[Math.min(target.index, items.length - 1)]
    const button = item?.querySelector<HTMLButtonElement>('button')
    ;(button ?? doneRef.current)?.focus()
  })

  // The mutations live here, not in the item: the item unmounts the moment it is
  // answered, and a hook in it would drop the toast and the rollback.
  const act = (item: CaptureReviewItem, answer: Answer) => {
    const index = Math.max(
      0,
      open.findIndex((o) => o.item.id === item.id),
    )
    const hide = (on: boolean) =>
      answer.kind === 'reject'
        ? setRefused((s) =>
            answer.lines.reduce((acc, l) => toggled(acc, pairKey(item.id, l.id), on), s),
          )
        : setSettled((s) => toggled(s, item.id, on))
    focusNext.current = { key: item.id, index }
    setAnswered(true)
    hide(true)
    setBusy((s) => toggled(s, item.id, true))
    const sent =
      answer.kind === 'link'
        ? link.mutateAsync({ id: item.id, entryId: answer.line.id })
        : answer.kind === 'reject'
          ? Promise.all(
              answer.lines.map((l) => reject.mutateAsync({ id: item.id, entryId: l.id })),
            )
          : discard.mutateAsync(item.id)
    sent
      .then(() => {
        if (answer.kind === 'discard') {
          toast('Listo: compra descartada', {
            action: { label: 'Deshacer', onClick: () => restore(item.id) },
          })
        } else {
          toast(
            answer.kind === 'link'
              ? 'Listo: la unimos con tu estado.'
              : 'Listo: no es ese movimiento.',
          )
        }
      })
      .catch((error: unknown) => {
        // The question comes back: focus returns to it rather than to one that left.
        focusNext.current = { key: item.id, index }
        hide(false)
        toast.error(problemMessage(error))
      })
      .finally(() => setBusy((s) => toggled(s, item.id, false)))
  }

  // The toast outlives the question, so this goes straight to the server; the
  // purchase shows again once the list has it back.
  const restore = (id: string) => {
    restoreCapture(id).then(
      () => {
        setSettled((s) => toggled(s, id, false))
        void invalidate()
      },
      () => toast('No se pudo deshacer. Intenta otra vez.'),
    )
  }

  if (review.isPending) return null
  // Without a first answer from the server nothing is known: never claim "none left".
  const failed = review.isError && !review.data
  if (failed && !keepWhenEmpty) return null
  if (open.length === 0 && !keepWhenEmpty && !answered) return null

  return (
    <SectionCard id={CAPTURES_ID} title="Compras por revisar" className={className}>
      {failed ? (
        <LoadError />
      ) : open.length === 0 ? (
        <p
          ref={doneRef}
          tabIndex={-1}
          className="text-body text-pretty text-muted-foreground outline-none"
        >
          No quedan compras por revisar.
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          <p className="text-small font-medium text-pretty text-muted-foreground">
            Las anotaste, pero tu estado no las confirma todavía.
          </p>
          <ul ref={listRef} className="flex flex-col gap-3">
            {open.map((o) => (
              <ReviewItem
                key={o.item.id}
                open={o}
                account={byId.get(o.item.account_id)}
                busy={waiting}
                onAnswer={(answer) => act(o.item, answer)}
                onPick={() => {
                  setPicking(o.item)
                  setPickerOpen(true)
                }}
              />
            ))}
          </ul>
        </div>
      )}
      <Dialog open={pickerOpen} onOpenChange={setPickerOpen}>
        {picking && (
          <LinePicker
            key={picking.id}
            item={picking}
            account={byId.get(picking.account_id)}
            onPick={(line) => {
              setPickerOpen(false)
              act(picking, { kind: 'link', line })
            }}
          />
        )}
      </Dialog>
    </SectionCard>
  )
}

function ReviewItem({
  open,
  account,
  busy,
  onAnswer,
  onPick,
}: {
  open: Open
  account?: Account
  /** An answer, about this purchase or another, is on its way: this one waits. */
  busy: boolean
  onAnswer: (answer: Answer) => void
  onPick: () => void
}) {
  const { item, candidates, near } = open
  const factsId = useId()
  // The closest line first; a "No" brings the next.
  const closest = near[0]
  // Nor does the second click of a double-click count, however fast the server: it
  // lands on whatever took the answered question's place.
  const wait = (run: () => void) => (event: MouseEvent) => {
    if (!busy && event.detail <= 1) run()
  }

  return (
    <li data-capture={item.id} className="flex flex-col gap-4 rounded-2xl bg-muted p-4">
      <Purchase id={factsId} item={item} account={account} />
      {item.state === 'printed' ? (
        twins(candidates).map((lines) => (
          <SameAmountLine
            key={lines[0].id}
            line={lines[0]}
            twins={lines.length}
            purchaseId={factsId}
            busy={busy}
            onYes={wait(() => onAnswer({ kind: 'link', line: lines[0] }))}
            onNo={wait(() => onAnswer({ kind: 'reject', lines }))}
          />
        ))
      ) : (
        <>
          {closest && (
            <CloseLine
              line={closest}
              purchaseId={factsId}
              busy={busy}
              onYes={wait(() => onAnswer({ kind: 'link', line: closest }))}
              onNo={wait(() => onAnswer({ kind: 'reject', lines: [closest] }))}
            />
          )}
          <NotInStatement
            purchaseId={factsId}
            busy={busy}
            likeliest={!closest}
            onDiscard={wait(() => onAnswer({ kind: 'discard' }))}
            onPick={wait(onPick)}
          />
        </>
      )}
    </li>
  )
}

/** What the owner noted: where, account and day, and how much. */
function Purchase({
  id,
  item,
  account,
}: {
  id: string
  item: CaptureReviewItem
  account?: Account
}) {
  return (
    <div id={id} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3">
      <div className="flex min-w-0 flex-col">
        <span className="line-clamp-2 text-body font-semibold break-words">
          {item.description}
        </span>
        <span className="truncate text-small font-medium text-muted-foreground">
          {[account && accountWithNumber(account), formatCivilDayMonth(item.booked_on)]
            .filter(Boolean)
            .join(' · ')}
        </span>
      </div>
      <Amount amount={item.amount} signed />
    </div>
  )
}

/** "¿Es este movimiento?": a line of the same amount, on the card colour. */
/**
 * The lines grouped by what the owner can tell apart: day, amount and text. Two equal
 * coffees on one day are one question, not two identical ones.
 */
function twins(lines: Entry[]): [Entry, ...Entry[]][] {
  const groups = new Map<string, [Entry, ...Entry[]]>()
  for (const line of lines) {
    const key = `${line.booked_on}|${line.amount.cents}|${line.description}`
    const group = groups.get(key)
    if (group) group.push(line)
    else groups.set(key, [line])
  }
  return [...groups.values()]
}

function SameAmountLine({
  line,
  twins = 1,
  purchaseId,
  busy,
  onYes,
  onNo,
}: {
  line: Entry
  /** How many lines just like this one the statement has. */
  twins?: number
  purchaseId: string
  busy: boolean
  onYes: Click
  onNo: Click
}) {
  const questionId = useId()
  const lineId = useId()
  return (
    <div role="group" aria-labelledby={questionId} className="flex flex-col gap-2">
      <p id={questionId} className="text-small font-semibold text-foreground">
        ¿Es este movimiento?
      </p>
      <div
        id={lineId}
        className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-2xl bg-card px-4 py-3"
      >
        <div className="flex min-w-0 flex-col">
          <span className="line-clamp-2 text-body font-medium break-words">
            {line.description || entryKindLabel(line.kind)}
          </span>
          <span className="truncate text-small font-medium text-muted-foreground">
            Tu estado · {formatCivilDayMonth(line.booked_on)}
            {twins > 1 && ` · ${twins} iguales`}
          </span>
        </div>
        <Amount amount={line.amount} signed />
      </div>
      <Answers
        yes="Sí, es este"
        describedBy={`${purchaseId} ${lineId}`}
        busy={busy}
        onYes={onYes}
        onNo={onNo}
      />
    </div>
  )
}

/** "Tu estado trae −$46.00 en CAFE UNIDO el 25 oct. ¿Es la misma compra?" */
function CloseLine({
  line,
  purchaseId,
  busy,
  onYes,
  onNo,
}: {
  line: Entry
  purchaseId: string
  busy: boolean
  onYes: Click
  onNo: Click
}) {
  const questionId = useId()
  return (
    <div role="group" aria-labelledby={questionId} className="flex flex-col gap-2">
      <p
        id={questionId}
        className="text-small font-medium text-pretty break-words text-foreground"
      >
        Tu estado trae{' '}
        <Amount amount={line.amount} size="inline" signed className="font-semibold" /> en{' '}
        {line.description || entryKindLabel(line.kind)} el{' '}
        {formatCivilDayMonth(line.booked_on)}. ¿Es la misma compra?
      </p>
      <Answers
        yes="Sí, es esta"
        describedBy={purchaseId}
        busy={busy}
        onYes={onYes}
        onNo={onNo}
      />
    </div>
  )
}

/** A missing purchase: discard it, or link it to a line nothing suggested. */
function NotInStatement({
  purchaseId,
  busy,
  likeliest,
  onDiscard,
  onPick,
}: {
  purchaseId: string
  busy: boolean
  /** Nothing comes close, so "Descartar" is the likeliest answer. */
  likeliest: boolean
  onDiscard: Click
  onPick: Click
}) {
  const questionId = useId()
  return (
    <div role="group" aria-labelledby={questionId} className="flex flex-col gap-2">
      <p id={questionId} className="text-small font-medium text-pretty text-foreground">
        Tu estado no la muestra. Si no se cobró, descártala.
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant={likeliest ? 'secondary' : 'ghost'}
          aria-describedby={purchaseId}
          aria-disabled={busy || undefined}
          className={cn(likeliest ? 'bg-card' : 'text-muted-foreground', 'hover:bg-line')}
          onClick={onDiscard}
        >
          Descartar
        </Button>
        <Button
          type="button"
          variant="ghost"
          aria-haspopup="dialog"
          aria-describedby={purchaseId}
          aria-disabled={busy || undefined}
          className="hover:bg-line"
          onClick={onPick}
        >
          Unir con otro
        </Button>
      </div>
    </div>
  )
}

/**
 * "Sí, es este" (secondary) and "No" (ghost). Never disabled while an answer is on
 * its way, which would drop the focus: aria-disabled, and the click waits.
 */
function Answers({
  yes,
  describedBy,
  busy,
  onYes,
  onNo,
}: {
  yes: string
  describedBy: string
  busy: boolean
  onYes: Click
  onNo: Click
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        type="button"
        variant="secondary"
        aria-describedby={describedBy}
        aria-disabled={busy || undefined}
        className="bg-card hover:bg-line"
        onClick={onYes}
      >
        {yes}
      </Button>
      <Button
        type="button"
        variant="ghost"
        aria-describedby={describedBy}
        aria-disabled={busy || undefined}
        className="text-muted-foreground hover:bg-line"
        onClick={onNo}
      >
        No
      </Button>
    </div>
  )
}

/**
 * "Unir con otro": the expense lines of the purchase's account around its day that no
 * other purchase has, closest day first (then closest amount). Picking one links it.
 * Below lg a bottom sheet, from lg a dialog; "Cancelar" is its only button, so the
 * page keeps its one primary. Focus goes back to "Unir con otro" when it closes
 * without a pick.
 */
function LinePicker({
  item,
  account,
  onPick,
}: {
  item: CaptureReviewItem
  account?: Account
  onPick: (line: Entry) => void
}) {
  const returnFocus = useReturnFocus()

  return (
    <ResponsiveDialogContent
      title="¿Con cuál movimiento la unimos?"
      description={
        <>
          {item.description} · <Amount amount={item.amount} size="inline" signed /> ·{' '}
          {formatCivilDayMonth(item.booked_on)}
          {account && ` · ${accountWithNumber(account)}`}
        </>
      }
      footer={
        <DialogClose asChild>
          <Button type="button" variant="secondary">
            Cancelar
          </Button>
        </DialogClose>
      }
      {...returnFocus}
    >
      <PickerLines item={item} onPick={onPick} />
    </ResponsiveDialogContent>
  )
}

/** The lines to pick from; mounted only while the picker is open, so it loads then. */
function PickerLines({
  item,
  onPick,
}: {
  item: CaptureReviewItem
  onPick: (line: Entry) => void
}) {
  const query: EntryQuery = {
    accountId: item.account_id,
    kinds: ['expense'],
    from: shiftDay(item.booked_on, -PICK_DAYS_BEFORE),
    to: shiftDay(item.booked_on, PICK_DAYS_AFTER),
    limit: 200,
  }
  // Not useEntries: its placeholder would show another purchase's lines while these
  // load, and a quick tap could pick one.
  const lines = useQuery({
    queryKey: keys.entries(query),
    queryFn: () => fetchEntries(query),
  })
  // One pick per opening: a double tap must not send a second link, whose refusal
  // would bring the purchase back.
  const picked = useRef(false)
  const pick = (line: Entry) => {
    if (picked.current) return
    picked.current = true
    onPick(line)
  }

  if (lines.isPending) return <Spinner className="self-center" />
  if (lines.isError) return <LoadError />

  const day = Date.parse(item.booked_on)
  const distance = (line: Entry) => Math.abs(Date.parse(line.booked_on) - day)
  const free = lines.data.entries
    // A pending purchase is not a line, and a line another purchase has is taken.
    .filter((line) => line.origin !== 'capture' && !line.noted)
    .sort(
      (a, b) =>
        distance(a) - distance(b) ||
        Math.abs(a.amount.cents - item.amount.cents) -
          Math.abs(b.amount.cents - item.amount.cents),
    )

  if (free.length === 0) {
    return (
      <p className="text-body text-pretty text-muted-foreground">
        Tu estado no trae otros gastos cerca de ese día.
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <p className="text-small font-medium text-pretty text-muted-foreground">
        Gastos de tu estado cerca de ese día.
      </p>
      <ul className="-mx-2 flex flex-col">
        {free.map((line) => (
          <li key={line.id}>
            <button
              type="button"
              onClick={() => pick(line)}
              className="grid min-h-14 w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-full px-4 py-2 text-left -outline-offset-2 transition-colors duration-180 ease-out hover:bg-muted active:bg-muted"
            >
              <span className="flex min-w-0 flex-col">
                <span className="truncate text-body font-medium">
                  {line.description || entryKindLabel(line.kind)}
                </span>
                <span className="truncate text-small font-medium text-muted-foreground">
                  {formatCivilDayMonth(line.booked_on)}
                </span>
              </span>
              <Amount amount={line.amount} signed />
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
