/**
 * <AccountOverview account menu onReview onAnchor onAddPurchase />
 *
 * The top of an account's page (spec C-Cuenta-Desktop / C-Cuenta-Mobile):
 *
 *   - From lg, the title row: the institution tile (56), the h1 (shortAccountLabel), the
 *     subtitle ("Gastos del mes · ···0932") and the overflow ellipsis at the right.
 *     Below lg the mobile bar carries the title and the overflow, and the h1 stays for
 *     screen readers.
 *   - The balance: its label ("Saldo", "Debes", "Total"; on mobile the subtitle takes
 *     its place, as in the artboard), the figure at title size with 22px cents, and
 *     the status chip (beside it from lg, under it on mobile). Unless the notice's
 *     "Revisar" already does, a ghost "Ver saldos del banco" (below lg; a broker's
 *     "Ver saldos" at every size) opens the review sheet.
 *   - The purchases the owner noted that no statement has printed yet, when there
 *     are some, in a muted line under the balance: "Pendiente −$12.50 · 3 compras ·
 *     Estimado $1,187.50" (a card: "Pendiente $12.50 · Con lo pendiente, debes
 *     $512.50"), with "Compras que anotaste y tu estado aún no trae." They are not in
 *     the balance, so they never touch its chip: never orange, never "No cuadra".
 *   - A card's statement (Corte · Pago · Mínimo), or a broker's Efectivo · Posiciones
 *     with "Sincronizar ahora".
 *   - The notice: "El banco dice $2,173.90 desde el 03 oct." with "Revisar" when it
 *     does not match, or a pending one when there is no starting balance.
 *
 * A card someone else pays has no balance of the owner's: its figure is "Este mes",
 * what it was used for this month, in the text colour, with "Cuenta en tus gastos, no
 * en lo que debes." under it. No chip, no "Ver saldos del banco", no statement, no
 * notice, and no "Pendiente" line: every purchase on it is pending forever and
 * already in that figure. Its one primary is "Anotar compra" (onAddPurchase), full
 * width below md.
 */
import { ReceiptIcon, ScaleIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import type { Account } from '@/api/types'
import { Amount, Notice, PageHeader, StatusChip, Tile } from '@/components/kit'
import { Button } from '@/components/ui/button'
import {
  accountStatus,
  amountOf,
  headline,
  PAID_BY_OTHER_NOTE,
  paidByOther,
  shortAccountLabel,
} from '@/lib/account'
import { useCheckpoints } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { AccountOverflow } from './AccountActions'
import { accountSubtitle, cardFacts, shortDate } from './bank'
import { InvestmentSync } from './Investments'

export function AccountOverview({
  account,
  menu,
  onReview,
  onAnchor,
  onAddPurchase,
}: {
  account: Account
  /** The overflow menu's items, from useAccountActions. */
  menu: ReactNode
  onReview: () => void
  onAnchor: () => void
  /** "Anotar compra", the primary of a card someone else pays. */
  onAddPurchase: () => void
}) {
  const status = accountStatus(account)
  const { amount, label } = headline(account)
  const subtitle = accountSubtitle(account)
  const brokerage = account.type === 'brokerage'
  const othersCard = paidByOther(account)
  // The review sheet holds the bank's figures (set or clear the starting balance,
  // delete one typed by hand). When nothing else opens it (no "Revisar" notice; and
  // below lg, no "Saldos del banco" card), a ghost button beside the chip does. A
  // broker has no side card at any size. A new, empty account has nothing to review
  // yet: the menu's "Saldo según tu banco" and the empty state cover it. A card
  // someone else pays has no bank figures at all.
  const reviewButton =
    !othersCard &&
    status?.kind !== 'no-cuadra' &&
    (account.anchored || account.movements > 0)

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        className="max-lg:sr-only"
        title={shortAccountLabel(account)}
        subtitle={subtitle}
        leading={<Tile institution={account.institution} size="lg" />}
        actions={
          <div className="hidden lg:flex">
            <AccountOverflow>{menu}</AccountOverflow>
          </div>
        }
      />

      <div className="flex flex-col gap-1">
        <p className="text-small font-medium text-muted-foreground">
          <span className="lg:hidden">
            {label === 'Saldo' ? subtitle : `${label} · ${subtitle}`}
          </span>
          <span className="max-lg:hidden">{label}</span>
        </p>
        <div className="flex flex-col items-start gap-3 lg:flex-row lg:flex-wrap lg:items-center lg:gap-4">
          {/* Muted while it is only a sum of movements. What a card someone else
              pays was used for is never that: nothing else could make it up. */}
          <Amount
            amount={amount}
            size="title"
            tone={account.anchored || othersCard ? 'auto' : 'muted'}
          />
          {(status || reviewButton) && (
            <div
              className={cn(
                'flex flex-wrap items-center gap-2',
                !status && !brokerage && 'lg:hidden',
              )}
            >
              {status && <StatusChip status={status} />}
              {reviewButton && (
                <Button
                  variant="ghost"
                  className={cn(!status && 'max-lg:-ml-3', !brokerage && 'lg:hidden')}
                  onClick={onReview}
                >
                  {brokerage ? 'Ver saldos' : 'Ver saldos del banco'}
                </Button>
              )}
            </div>
          )}
        </div>
        <PendingPurchasesLine account={account} />
        {othersCard && (
          <p className="pt-1 text-small font-medium text-muted-foreground">
            {PAID_BY_OTHER_NOTE}
          </p>
        )}
      </div>

      {othersCard ? (
        <Button size="lg" className="md:h-11 md:self-start" onClick={onAddPurchase}>
          <ReceiptIcon aria-hidden="true" />
          Anotar compra
        </Button>
      ) : (
        <Facts account={account} />
      )}
      {brokerage && <InvestmentSync account={account} />}

      {status?.kind === 'no-cuadra' && (
        <Notice
          className="lg:max-w-170"
          title={
            <>
              {brokerage ? 'Interactive Brokers' : 'El banco'} dice{' '}
              <Amount amount={status.stated} size="inline" /> desde el{' '}
              {shortDate(status.asOf)}.
            </>
          }
          description={
            <span className="max-lg:hidden">
              Revisa lo que entró y salió desde esa fecha.
            </span>
          }
          action={{ label: 'Revisar', onClick: onReview }}
        />
      )}
      {status?.kind === 'sin-partida' && (
        <Notice
          tone="pending"
          icon={ScaleIcon}
          className="lg:max-w-170"
          title="Esta cuenta no tiene saldo inicial"
          description="Anota el saldo que te da tu banco y te decimos si cuadra."
          action={{ label: 'Anotar saldo', onClick: onAnchor }}
        />
      )}
    </div>
  )
}

/**
 * What the noted purchases come to, beside the balance that does not have them yet.
 * A bank account: "Pendiente −$12.50 · 3 compras · Estimado $1,187.50". A card, whose
 * figure is what is owed: "Pendiente $12.50 · Con lo pendiente, debes $512.50". The
 * estimate is only there when the balance is anchored; without it the count stands
 * in. Never on a card someone else pays: no statement will ever print its purchases,
 * and what they come to this month is already its figure.
 */
function PendingPurchasesLine({ account }: { account: Account }) {
  const pending = account.pending
  if (!pending || pending.count === 0 || account.type === 'brokerage') return null
  if (paidByOther(account)) return null
  const card = account.class === 'liability'
  const estimated = account.estimated_balance
  const count = `${pending.count} ${pending.count === 1 ? 'compra' : 'compras'}`

  let rest: ReactNode = count
  if (estimated && card) {
    // What is owed is the balance's opposite; a card in the owner's favour says so.
    const owed = -estimated.cents
    rest =
      owed >= 0 ? (
        <>
          Con lo pendiente, debes <Amount amount={owed} size="inline" />
        </>
      ) : (
        <>
          Con lo pendiente, tienes <Amount amount={-owed} size="inline" /> a favor
        </>
      )
  } else if (estimated) {
    rest = (
      <>
        {count} · Estimado <Amount amount={estimated} size="inline" />
      </>
    )
  }

  return (
    <div className="flex flex-col pt-1 text-small font-medium text-muted-foreground">
      <p>
        Pendiente{' '}
        <Amount amount={card ? -pending.amount.cents : pending.amount} size="inline" />
        {' · '}
        {rest}
      </p>
      <p>Compras que anotaste y tu estado aún no trae.</p>
    </div>
  )
}

/** A card's Corte · Pago · Mínimo, or a broker's Efectivo · Posiciones. */
function Facts({ account }: { account: Account }) {
  const card = account.type === 'credit_card'
  const checkpoints = useCheckpoints(account.id)
  if (card) {
    const facts = cardFacts(account, checkpoints.data?.checkpoints)
    if (!facts) return null
    return (
      <FactList
        items={[
          { term: 'Corte', value: shortDate(facts.closingOn) },
          facts.dueOn && { term: 'Pago', value: shortDate(facts.dueOn) },
          facts.minimum && { term: 'Mínimo', value: <Amount amount={facts.minimum} /> },
        ]}
      />
    )
  }
  if (account.type === 'brokerage') {
    return (
      <FactList
        items={[
          { term: 'Efectivo', value: <Amount amount={account.balance} /> },
          {
            term: 'Posiciones',
            value: <Amount amount={account.holdings ?? amountOf(0)} />,
          },
        ]}
      />
    )
  }
  return null
}

function FactList({
  items,
}: {
  items: ({ term: string; value: ReactNode } | null | false | '')[]
}) {
  const shown = items.filter((item) => item !== null && item !== false && item !== '')
  return (
    <dl className="grid max-w-md grid-cols-3 gap-4 lg:gap-6">
      {shown.map((item) => (
        <div key={item.term} className="flex min-w-0 flex-col">
          <dt className="text-small font-medium text-muted-foreground">{item.term}</dt>
          <dd className="text-body font-semibold">{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}
