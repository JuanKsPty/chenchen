/**
 * What the import dialog shows after a file is read.
 *
 *   <ImportedOutcome result accounts onNavigate />
 *       After "Importar": spec voice.importResult. "Listo: 12 movimientos nuevos",
 *       "En Ahorros ···4417 (01–30 sept). 3 ya estaban y no los repetimos.", the
 *       bank's opening and closing balance, and the account's chip once the list
 *       has refreshed (Cuadra, No cuadra · $25.00 with "Revisar", Sin saldo
 *       inicial). A card file with no balance asks for "Saldo según tu banco"
 *       right there (CheckpointDialog, as the starting balance). One account is
 *       laid out like the brand sheet (illustration, centred); several get a
 *       block each.
 *   <CheckedOutcome report accounts lockedTo? />
 *       After "Solo revisar" (the dialog titles it "Esto dice el archivo"): what
 *       the file says, written nowhere. Period,
 *       movements, money in and out, the bank's balances and whether the lines add
 *       up to them.
 *   importNeedsStep(result, accounts)
 *       true when the result carries its own next step (a card that needs its
 *       starting balance, or an account that does not match), so the dialog's
 *       "Listo" steps down from primary. Once the starting balance is saved the
 *       step is done: the box goes, the account's chip shows and "Listo" is the
 *       primary again.
 *   importHeadline(result)
 *       "Listo: 12 movimientos nuevos", also used for the toast when the dialog
 *       was closed before the import finished.
 *   accountName(account)
 *       "Gastos del mes ···0932" (the owner's alias) or "Ahorros ···4417": how the
 *       whole flow names an account, so it matches the row on Cuentas.
 */
import { ArrowRightIcon, CircleAlertIcon, CircleCheckIcon, InfoIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import type {
  Account,
  Amount as AmountValue,
  ImportAccountResult,
  ImportResult,
  StatementFileReport,
  StatementReport,
} from '@/api/types'
import { Illustration } from '@/components/brand'
import { CheckpointDialog } from '@/components/CheckpointDialog'
import { Amount, StatusChip } from '@/components/kit'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { DialogTitle } from '@/components/ui/dialog'
import { accountStatus, accountWithNumber } from '@/lib/account'
import { accountTypeLabel, formatRange, institutionLabel, numberShort } from '@/lib/format'
import { accountHref } from '@/lib/nav'
import { cn } from '@/lib/utils'

function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`
}

function sum(result: ImportResult, pick: (a: ImportAccountResult) => number): number {
  return result.accounts.reduce((total, account) => total + pick(account), 0)
}

export function importHeadline(result: ImportResult): string {
  if (result.unchanged) return 'Ya tenías este archivo'
  const fresh = sum(result, (a) => a.new)
  return fresh === 0
    ? 'No había nada nuevo'
    : `Listo: ${plural(fresh, 'movimiento nuevo', 'movimientos nuevos')}`
}

export function importNeedsStep(result: ImportResult, accounts: Account[]): boolean {
  if (result.unchanged) return false
  return result.accounts.some((a) => {
    const account = accounts.find((x) => x.id === a.account_id)
    if (needsOpening(a, account)) return true
    return account ? accountStatus(account)?.kind === 'no-cuadra' : false
  })
}

/**
 * The account as the owner knows it, with its number: "Gastos del mes ···0932"
 * when they named it, else "Ahorros ···4417" / "Tarjeta ···1111".
 */
export function accountName(account: Account): string {
  const alias = account.alias?.trim()
  return alias
    ? `${alias} ${numberShort(account.external_number)}`
    : accountWithNumber(account)
}

/** A card file with no balance, until a starting balance is saved for it. */
function needsOpening(result: ImportAccountResult, account: Account | undefined): boolean {
  return result.needs_opening && !account?.anchored
}

/** The statement's period, "01–30 sept", when the file has one. */
function period(start: string | null | undefined, end: string | null | undefined) {
  return start && end ? formatRange(start, end) : ''
}

// --- after Importar ------------------------------------------------------------

export function ImportedOutcome({
  result,
  accounts,
  onNavigate,
}: {
  result: ImportResult
  accounts: Account[]
  /** Called when a link inside leaves for another page, to close the dialog. */
  onNavigate: () => void
}) {
  const find = (id: string) => accounts.find((a) => a.id === id)

  if (result.unchanged) {
    const account = result.accounts[0] && find(result.accounts[0].account_id)
    return (
      <Outcome
        title={importHeadline(result)}
        text={
          account
            ? `No cambió nada: sus movimientos ya estaban en ${accountName(account)}.`
            : 'No cambió nada: sus movimientos ya estaban.'
        }
      />
    )
  }

  if (result.accounts.length === 1) {
    const only = result.accounts[0]!
    const account = find(only.account_id)
    const balance = only.opening && only.closing && (
      <BankBalance opening={only.opening} closing={only.closing} centered />
    )
    // "Anota lo que debes" below already says it has no starting balance.
    const chip = hasChip(account) && !needsOpening(only, account) && (
      <AccountChip account={account} onNavigate={onNavigate} />
    )
    return (
      <div className="flex flex-col gap-4">
        <Outcome
          illustration
          title={importHeadline(result)}
          text={sentence(only, account)}
          details={
            (balance || chip) && (
              <>
                {balance}
                {chip}
              </>
            )
          }
        />
        <Follow result={only} account={account} />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <Outcome
        title={importHeadline(result)}
        text={`En ${result.accounts.length} cuentas.`}
      />
      {result.accounts.map((one) => {
        const account = find(one.account_id)
        return (
          <section
            key={one.account_id}
            className="flex flex-col gap-3 rounded-2xl bg-muted p-4"
            aria-label={name(one, account)}
          >
            <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2">
              <div className="flex min-w-0 flex-col">
                <p className="truncate text-body font-semibold">{name(one, account)}</p>
                <p className="text-small font-medium text-muted-foreground">
                  {[
                    period(one.period_start, one.period_end),
                    plural(one.new, 'nuevo', 'nuevos'),
                    one.duplicates > 0 && plural(one.duplicates, 'ya estaba', 'ya estaban'),
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </p>
              </div>
              {!needsOpening(one, account) && (
                <AccountChip account={account} onNavigate={onNavigate} />
              )}
            </div>
            {one.opening && one.closing && (
              <BankBalance opening={one.opening} closing={one.closing} />
            )}
            <Follow result={one} account={account} />
          </section>
        )
      })}
    </div>
  )
}

/** "Ahorros ···4417", or the file's own name for it until the list has it. */
function name(result: ImportAccountResult, account: Account | undefined): string {
  return account ? accountName(account) : result.display_name
}

/** The one-line summary under the headline, per voice.importResult. */
function sentence(result: ImportAccountResult, account: Account | undefined): string {
  const where = name(result, account)
  const when = period(result.period_start, result.period_end)
  const place = when ? `${where} (${when})` : where
  if (result.new === 0) {
    return result.duplicates > 0
      ? `${result.duplicates === 1 ? 'El movimiento' : `Los ${result.duplicates} movimientos`} de ${place} ya ${result.duplicates === 1 ? 'estaba' : 'estaban'}.`
      : `El archivo de ${place} no trae movimientos.`
  }
  const first = result.created ? `Agregamos ${place} a tus cuentas.` : `En ${place}.`
  const repeated =
    result.duplicates === 0
      ? ''
      : result.duplicates === 1
        ? ' 1 ya estaba y no lo repetimos.'
        : ` ${result.duplicates} ya estaban y no los repetimos.`
  return first + repeated
}

function hasChip(account: Account | undefined): boolean {
  const status = account ? accountStatus(account) : null
  return Boolean(status && status.kind !== 'sincronizado')
}

/** The account's chip after the import: Cuadra, No cuadra · $25.00 or Sin saldo inicial. */
function AccountChip({
  account,
  onNavigate,
}: {
  account: Account | undefined
  onNavigate: () => void
}) {
  const status = account ? accountStatus(account) : null
  if (!account || !status || status.kind === 'sincronizado') return null
  if (status.kind !== 'no-cuadra') return <StatusChip status={status} />
  return (
    <div className="flex flex-wrap items-center justify-center gap-1">
      <StatusChip status={status} />
      <Button asChild variant="ghost">
        <Link to={accountHref(account.id, { review: true })} onClick={onNavigate}>
          Revisar
        </Link>
      </Button>
    </div>
  )
}

/** What still needs doing after the import: a starting balance, a break, warnings. */
function Follow({
  result,
  account,
}: {
  result: ImportAccountResult
  account: Account | undefined
}) {
  return (
    <>
      {needsOpening(result, account) && (
        <div className="flex flex-col gap-3 rounded-2xl bg-muted p-4">
          <div className="flex flex-col">
            <p className="text-body font-semibold text-foreground">Anota lo que debes</p>
            <p className="text-small font-medium text-pretty text-muted-foreground">
              Este archivo no trae saldo. Escribe el de tu estado de cuenta y te decimos si
              cuadra.
            </p>
          </div>
          {account && (
            <CheckpointDialog
              account={account}
              anchor
              trigger={
                <Button className="w-full sm:w-auto sm:self-start">
                  Saldo según tu banco
                </Button>
              }
            />
          )}
        </div>
      )}
      {result.chain_break && <LineBreak line={result.chain_break} />}
      {result.warnings.length > 0 && <Warnings warnings={result.warnings} />}
    </>
  )
}

// --- after Solo revisar ----------------------------------------------------------

export function CheckedOutcome({
  report,
  accounts,
  lockedTo,
}: {
  report: StatementFileReport
  accounts: Account[]
  /** The account an import from here is locked to, if any. */
  lockedTo?: Account
}) {
  return (
    <div className="flex flex-col gap-4">
      {report.statements.map((statement) => (
        <StatementSummary
          key={`${statement.institution}-${statement.external_number}`}
          statement={statement}
          account={matchAccount(statement, accounts)}
          lockedTo={lockedTo}
        />
      ))}
    </div>
  )
}

/** The account a statement belongs to, when it is already in chenchén. */
export function matchAccount(statement: StatementReport, accounts: Account[]) {
  return accounts.find(
    (a) =>
      a.institution === statement.institution &&
      a.external_number === statement.external_number,
  )
}

function StatementSummary({
  statement,
  account,
  lockedTo,
}: {
  statement: StatementReport
  account: Account | undefined
  lockedTo?: Account
}) {
  const title = account
    ? accountName(account)
    : `${accountTypeLabel(statement.type)} ${numberShort(statement.external_number)}`
  const elsewhere = lockedTo && account?.id !== lockedTo.id
  const balanced = statement.opening && statement.closing && !statement.chain_break

  return (
    <section aria-label={title} className="flex flex-col gap-3 rounded-2xl bg-muted p-4">
      <div className="flex min-w-0 flex-col">
        <p className="truncate text-body font-semibold">{title}</p>
        <p className="text-small font-medium text-muted-foreground">
          {institutionLabel(statement.institution)} ·{' '}
          {account ? 'Ya está en tus cuentas' : 'Cuenta nueva'}
        </p>
      </div>

      <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-4 gap-y-1">
        <Fact label="Período">
          {period(statement.period_start, statement.period_end) || 'Sin fechas'}
        </Fact>
        <Fact label="Movimientos">{statement.line_count}</Fact>
        <Fact label="Entradas">
          <Amount amount={statement.money_in} signed />
        </Fact>
        <Fact label="Salidas">
          <Amount amount={-statement.money_out.cents} signed />
        </Fact>
        <Fact label="Saldo del banco">
          {statement.opening && statement.closing ? (
            <BalancePair opening={statement.opening} closing={statement.closing} />
          ) : (
            <span className="text-muted-foreground">No lo trae</span>
          )}
        </Fact>
      </dl>

      {balanced && (
        <p className="flex items-center gap-2 text-small font-semibold text-positive">
          <CircleCheckIcon aria-hidden="true" className="size-4 shrink-0" />
          Los movimientos cuadran con el saldo del banco.
        </p>
      )}
      {statement.class === 'liability' && !statement.opening && (
        <p className="text-small font-medium text-pretty text-muted-foreground">
          Este archivo no trae saldo. Al importarlo, anota lo que debes según tu estado de
          cuenta.
        </p>
      )}
      {elsewhere && (
        <Alert variant="attention">
          <CircleAlertIcon />
          <AlertTitle>Este archivo es de otra cuenta</AlertTitle>
          <AlertDescription>
            Desde aquí solo importas a {accountName(lockedTo)}.
          </AlertDescription>
        </Alert>
      )}
      {statement.chain_break && <LineBreak line={statement.chain_break} />}
      {statement.warnings.length > 0 && <Warnings warnings={statement.warnings} />}
    </section>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-small font-medium text-muted-foreground">{label}</dt>
      <dd className="text-right text-body font-semibold tabular-nums">{children}</dd>
    </>
  )
}

// --- shared pieces -------------------------------------------------------------

/**
 * "Saldo del banco" and "$16,868.02 → $18,240.15" (a lucide arrow, not the glyph):
 * stacked and centred under the headline, or label and figures on one line.
 */
function BankBalance({
  opening,
  closing,
  centered = false,
}: {
  opening: AmountValue
  closing: AmountValue
  centered?: boolean
}) {
  return (
    <div
      className={cn(
        'flex',
        centered
          ? 'flex-col items-center'
          : 'flex-wrap items-baseline justify-between gap-x-4',
      )}
    >
      <span className="text-small font-medium text-muted-foreground">Saldo del banco</span>
      <BalancePair opening={opening} closing={closing} />
    </div>
  )
}

function BalancePair({ opening, closing }: { opening: AmountValue; closing: AmountValue }) {
  return (
    <span className="inline-flex flex-wrap items-center justify-end gap-x-1.5 text-body font-semibold">
      <Amount amount={opening} size="inline" />
      <ArrowRightIcon
        aria-hidden="true"
        className="size-4 shrink-0 text-muted-foreground"
      />
      <span className="sr-only">a</span>
      <Amount amount={closing} size="inline" />
    </span>
  )
}

/** The first line where the bank's running balance and the sum part ways. */
function LineBreak({
  line,
}: {
  line: { line_no: number; bank: AmountValue; computed: AmountValue }
}) {
  return (
    <Alert variant="attention">
      <CircleAlertIcon />
      <AlertTitle>No cuadra en la línea {line.line_no}</AlertTitle>
      <AlertDescription>
        El banco dice <Amount amount={line.bank} size="inline" /> y la suma da{' '}
        <Amount amount={line.computed} size="inline" />. Revisa el archivo.
      </AlertDescription>
    </Alert>
  )
}

function Warnings({ warnings }: { warnings: string[] }) {
  return (
    <ul className="flex flex-col gap-2">
      {warnings.map((warning) => (
        <li
          key={warning}
          className="grid grid-cols-[1.25rem_minmax(0,1fr)] gap-2 text-small font-medium text-pretty text-foreground"
        >
          <InfoIcon aria-hidden="true" className="mt-0.5 size-4 text-muted-foreground" />
          {warning}
        </li>
      ))}
    </ul>
  )
}

/**
 * The centred headline block: illustration, title, sentence, then details. The
 * title is the dialog's own <DialogTitle>: the result view draws no header.
 */
function Outcome({
  title,
  text,
  illustration = false,
  details,
}: {
  title: string
  text: string
  illustration?: boolean
  details?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center gap-3 py-2 text-center">
      {illustration && <Illustration name="import" className="mb-1" />}
      <DialogTitle tabIndex={-1} className="pr-0 font-display text-balance outline-none">
        {title}
      </DialogTitle>
      <p className="max-w-90 text-body text-pretty text-muted-foreground">{text}</p>
      {details && <div className="mt-2 flex flex-col items-center gap-2">{details}</div>}
    </div>
  )
}
