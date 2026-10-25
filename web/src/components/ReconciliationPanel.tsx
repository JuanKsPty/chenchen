/**
 * <ReconciliationPanel account />
 *
 * The second half of the review sheet: each imported statement against what its
 * movements add up to, and, for a bank that prints a running balance, the lines with
 * both balances, opened where they part. Nothing here corrects anything: a difference
 * is shown so its cause can be found (a statement never imported, a line missing, a
 * starting balance typed wrong).
 *
 * Statements: newest first, "01–31 jul · 7 movimientos · Cierre $3,104.84" with
 * "Cuadra" or "No cuadra · $X", a missing period and the importer's warnings.
 * Línea por línea: a window of lines (around the first break, or the latest ones)
 * that opens to all of them.
 */
import { FileTextIcon } from 'lucide-react'
import { useId, useState } from 'react'

import type { Account, ReconciliationLine, StatementCheck } from '@/api/types'
import { Amount, Tile } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatRange } from '@/lib/format'
import { useReconciliation } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { LOAD_FAILED, shortDate, statedAmount } from './account/bank'

export function ReconciliationPanel({ account }: { account: Account }) {
  const reconciliation = useReconciliation(account.id)
  const data = reconciliation.data

  if (reconciliation.isLoading) return <Skeleton className="h-40 w-full" />
  if (reconciliation.isError) {
    return <p className="text-body text-pretty text-muted-foreground">{LOAD_FAILED}</p>
  }
  if (!data) return null

  const statements = [...data.statements].reverse()
  const withBank = data.lines.some((line) => line.bank)

  return (
    <div className="flex flex-col gap-6">
      {statements.length > 0 && <Statements account={account} statements={statements} />}
      {withBank && (
        <Lines account={account} lines={data.lines} firstBreak={data.first_break} />
      )}
    </div>
  )
}

function Statements({
  account,
  statements,
}: {
  account: Account
  statements: StatementCheck[]
}) {
  const headingId = useId()
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-1">
      <h3 id={headingId} className="flex min-h-11 items-center text-body font-semibold">
        Estados de cuenta
      </h3>
      <ul className="-mx-4 flex flex-col">
        {statements.map((statement) => (
          <li key={statement.id}>
            <StatementRow account={account} statement={statement} />
          </li>
        ))}
      </ul>
    </section>
  )
}

function StatementRow({
  account,
  statement,
}: {
  account: Account
  statement: StatementCheck
}) {
  const checks = [statement.opening, statement.closing].filter(
    (check) => check !== undefined,
  )
  const off = Math.max(0, ...checks.map((check) => Math.abs(check.difference.cents)))
  const notes = [
    ...(statement.gap ? ['Falta un periodo antes de este estado.'] : []),
    ...statement.warnings,
  ]

  return (
    <div className="grid min-h-16 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 md:min-h-14 md:py-2">
      <Tile icon={FileTextIcon} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-body font-medium">
          {formatRange(statement.period_start, statement.period_end)}
        </span>
        <span className="truncate text-small font-medium text-muted-foreground">
          {statement.line_count === 1
            ? '1 movimiento'
            : `${statement.line_count} movimientos`}
          {statement.closing && (
            <>
              {' · Cierre '}
              <Amount
                amount={statedAmount(account, statement.closing.checkpoint.balance)}
                size="inline"
              />
            </>
          )}
        </span>
      </span>
      <span className="text-right text-small font-semibold whitespace-nowrap">
        {checks.length === 0 ? (
          <span className="font-medium text-muted-foreground">Sin saldo impreso</span>
        ) : off === 0 ? (
          <span className="text-positive">Cuadra</span>
        ) : (
          <span className="text-attention">
            No cuadra · <Amount amount={off} size="inline" />
          </span>
        )}
      </span>
      {notes.length > 0 && (
        <ul className="col-start-2 col-end-4 flex flex-col text-small font-medium text-attention">
          {notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** How many lines show before "Ver todas". */
const WINDOW = 8

function Lines({
  account,
  lines,
  firstBreak,
}: {
  account: Account
  lines: ReconciliationLine[]
  firstBreak?: string
}) {
  const headingId = useId()
  const [all, setAll] = useState(false)
  const breakAt = firstBreak ? lines.findIndex((line) => line.id === firstBreak) : -1
  const start = breakAt >= 0 ? Math.max(0, breakAt - 3) : Math.max(0, lines.length - WINDOW)
  const shown = all ? lines : lines.slice(start, start + WINDOW)

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-1">
      <div className="flex flex-col gap-0.5 py-2.5">
        <h3 id={headingId} className="text-body font-semibold">
          Línea por línea
        </h3>
        <p
          className={cn(
            'text-small font-medium text-pretty',
            breakAt >= 0 ? 'text-attention' : 'text-muted-foreground',
          )}
        >
          {breakAt >= 0
            ? 'La diferencia empieza en la línea marcada.'
            : 'Cada saldo que imprimió el banco coincide con tus movimientos.'}
        </p>
      </div>
      <ol className="-mx-2 flex flex-col">
        {shown.map((line) => (
          <li key={line.id}>
            <LineRow account={account} line={line} marked={line.id === firstBreak} />
          </li>
        ))}
      </ol>
      {lines.length > shown.length && (
        <Button variant="ghost" className="-ml-3 self-start" onClick={() => setAll(true)}>
          Ver todas las líneas ({lines.length})
        </Button>
      )}
    </section>
  )
}

/** Balances read as the bank prints them (a card's debt positive); the line's own amount keeps its sign. */
function LineRow({
  account,
  line,
  marked,
}: {
  account: Account
  line: ReconciliationLine
  marked: boolean
}) {
  const off = line.bank && line.difference && line.difference.cents !== 0

  return (
    <div
      className={cn(
        'grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 rounded-2xl px-2 py-2.5 md:py-2',
        marked && 'bg-attention-soft',
      )}
    >
      <span className="truncate text-body font-medium">{line.description}</span>
      <Amount amount={line.amount} signed className="text-right" />
      <span className="truncate text-small font-medium text-muted-foreground">
        {shortDate(line.booked_on)} · Saldo{' '}
        <Amount amount={statedAmount(account, line.computed)} size="inline" />
      </span>
      {off && line.bank ? (
        <span className="text-right text-small font-semibold whitespace-nowrap text-attention">
          Banco <Amount amount={statedAmount(account, line.bank)} size="inline" />
        </span>
      ) : (
        <span />
      )}
    </div>
  )
}
