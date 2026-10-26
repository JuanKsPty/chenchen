/**
 * <TransferSuggestions keepWhenEmpty? className? />
 *
 *   keepWhenEmpty  show the section even with nothing to decide (the page was opened
 *                  at #transferencias, or the last pair was just decided): it then
 *                  says "No quedan transferencias por confirmar."
 *
 * The section `#transferencias` (TRANSFERS_HREF; Inicio's pending notice links here).
 * Pairs of movements that look like one transfer between the owner's own accounts
 * but were not clear enough to confirm alone, one block each: what left and what
 * arrived, with account and day, then "Confirmar" (both leave spending and income;
 * secondary, so several pairs never show several primaries) and "No es" (ghost).
 * Not tied to the month on screen: a pair from September stays here in October
 * until someone answers.
 *
 * A decided pair disappears at once; a toast says what happened, and focus moves to
 * the next pair's "Confirmar", or to the "none left" line. On failure the pair comes
 * back, focused, and the toast says what failed. If the pairs cannot load, the
 * section shows the error line when it was asked for (#transferencias) and stays
 * away otherwise; it never claims there is nothing left.
 */
import { useEffect, useId, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { Account, Entry, TransferSuggestion } from '@/api/types'
import { Amount, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { accountWithNumber } from '@/lib/account'
import { formatCivilDayMonth } from '@/lib/format'
import { TRANSFERS_HREF } from '@/lib/nav'
import { useAccounts, useDecideTransfer, useTransferSuggestions } from '@/lib/queries'
import { LoadError } from './LoadError'

export const TRANSFERS_ID = TRANSFERS_HREF.slice(TRANSFERS_HREF.indexOf('#') + 1)

const keyOf = (s: TransferSuggestion) => `${s.out.id}:${s.in.id}`

export function TransferSuggestions({
  keepWhenEmpty = false,
  className,
}: {
  keepWhenEmpty?: boolean
  className?: string
}) {
  const suggestions = useTransferSuggestions()
  const accounts = useAccounts()
  const decide = useDecideTransfer()
  const [decided, setDecided] = useState<ReadonlySet<string>>(() => new Set())
  // Where focus goes once the list re-renders: the first pair left after an answer,
  // or the pair (by key) that came back after a failure.
  const focusNext = useRef<{ key?: string } | null>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const doneRef = useRef<HTMLParagraphElement>(null)

  const pending = (suggestions.data?.suggestions ?? []).filter(
    (s) => !decided.has(keyOf(s)),
  )
  const byId = new Map((accounts.data?.accounts ?? []).map((a) => [a.id, a]))

  // After a decision the pair is gone: give focus somewhere that still exists.
  useEffect(() => {
    const target = focusNext.current
    if (!target) return
    focusNext.current = null
    const items = Array.from(listRef.current?.children ?? [])
    const item = target.key
      ? items.find((li) => li.getAttribute('data-pair') === target.key)
      : items[0]
    const button = item?.querySelector<HTMLButtonElement>('button')
    ;(button ?? doneRef.current)?.focus()
  }, [pending.length])

  const markDecided = (key: string, value: boolean) => {
    setDecided((previous) => {
      const copy = new Set(previous)
      if (value) copy.add(key)
      else copy.delete(key)
      return copy
    })
  }

  // The mutation lives here, not in the row: the row unmounts the moment it is
  // answered, and a hook in it would drop the toast and the rollback.
  const answer = (suggestion: TransferSuggestion, confirm: boolean) => {
    const key = keyOf(suggestion)
    focusNext.current = {}
    markDecided(key, true)
    decide
      .mutateAsync({
        out_entry_id: suggestion.out.id,
        in_entry_id: suggestion.in.id,
        confirm,
      })
      .then(() => {
        toast(
          confirm
            ? 'Listo: es una transferencia, no un gasto.'
            : 'Listo: no es una transferencia.',
        )
      })
      .catch((error: unknown) => {
        // The pair comes back: focus returns to it rather than to a line that left.
        focusNext.current = { key }
        markDecided(key, false)
        toast.error(
          error instanceof ApiError
            ? error.message
            : 'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.',
        )
      })
  }

  if (suggestions.isPending) return null
  // Without a first answer from the server nothing is known: never claim "none left".
  const failed = suggestions.isError && !suggestions.data
  if (failed && !keepWhenEmpty) return null
  if (pending.length === 0 && !keepWhenEmpty && decided.size === 0) return null

  return (
    <SectionCard
      id={TRANSFERS_ID}
      title="¿Son transferencias entre tus cuentas?"
      className={className}
    >
      {failed ? (
        <LoadError />
      ) : pending.length === 0 ? (
        <p
          ref={doneRef}
          tabIndex={-1}
          className="text-body text-pretty text-muted-foreground outline-none"
        >
          No quedan transferencias por confirmar.
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          <p className="text-small font-medium text-pretty text-muted-foreground">
            Si lo son, no cuentan como gasto ni como entrada.
          </p>
          <ul ref={listRef} className="flex flex-col gap-3">
            {pending.map((s) => (
              <SuggestionItem
                key={keyOf(s)}
                pairKey={keyOf(s)}
                suggestion={s}
                accounts={byId}
                onAnswer={(confirm) => answer(s, confirm)}
              />
            ))}
          </ul>
        </div>
      )}
    </SectionCard>
  )
}

function SuggestionItem({
  pairKey,
  suggestion,
  accounts,
  onAnswer,
}: {
  pairKey: string
  suggestion: TransferSuggestion
  accounts: Map<string, Account>
  onAnswer: (confirm: boolean) => void
}) {
  const legsId = useId()

  return (
    <li data-pair={pairKey} className="flex flex-col gap-3 rounded-2xl bg-muted p-4">
      <div id={legsId} className="flex flex-col gap-3">
        <Leg
          verb="Sale de"
          entry={suggestion.out}
          account={accounts.get(suggestion.out.account_id)}
        />
        <Leg
          verb="Entra a"
          entry={suggestion.in}
          account={accounts.get(suggestion.in.account_id)}
        />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {/* Secondary, not primary: a list of pairs would otherwise show a primary
            per pair, and the view keeps one (the bar's "Importar estado"). On the
            pair's surface2 block it takes the card colour, so it keeps its shape. */}
        <Button
          type="button"
          variant="secondary"
          aria-describedby={legsId}
          className="bg-card hover:bg-line"
          onClick={() => onAnswer(true)}
        >
          Confirmar
        </Button>
        <Button
          type="button"
          variant="ghost"
          aria-describedby={legsId}
          className="text-muted-foreground hover:bg-line"
          onClick={() => onAnswer(false)}
        >
          No es
        </Button>
      </div>
    </li>
  )
}

function Leg({ verb, entry, account }: { verb: string; entry: Entry; account?: Account }) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3">
      <div className="flex min-w-0 flex-col">
        <span className="line-clamp-2 text-body font-medium break-words">
          {entry.description}
        </span>
        <span className="truncate text-small font-medium text-muted-foreground">
          {verb} {account ? accountWithNumber(account) : 'otra cuenta'} ·{' '}
          {formatCivilDayMonth(entry.booked_on)}
        </span>
      </div>
      <Amount amount={entry.amount} signed />
    </div>
  )
}
