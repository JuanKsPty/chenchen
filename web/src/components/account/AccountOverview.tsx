/**
 * <AccountOverview account menu onReview onAnchor />
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
 *   - A card's statement (Corte · Pago · Mínimo), or a broker's Efectivo · Posiciones
 *     with "Sincronizar ahora".
 *   - The notice: "El banco dice $2,173.90 desde el 03 oct." with "Revisar" when it
 *     does not match, or a pending one when there is no starting balance.
 */
import { ScaleIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import type { Account } from '@/api/types'
import { Amount, Notice, PageHeader, StatusChip, Tile } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { accountStatus, amountOf, headline, shortAccountLabel } from '@/lib/account'
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
}: {
  account: Account
  /** The overflow menu's items, from useAccountActions. */
  menu: ReactNode
  onReview: () => void
  onAnchor: () => void
}) {
  const status = accountStatus(account)
  const { amount, label } = headline(account)
  const subtitle = accountSubtitle(account)
  const brokerage = account.type === 'brokerage'
  // The review sheet holds the bank's figures (set or clear the starting balance,
  // delete one typed by hand). When nothing else opens it (no "Revisar" notice; and
  // below lg, no "Saldos del banco" card), a ghost button beside the chip does. A
  // broker has no side card at any size. A new, empty account has nothing to review
  // yet: the menu's "Saldo según tu banco" and the empty state cover it.
  const reviewButton =
    status?.kind !== 'no-cuadra' && (account.anchored || account.movements > 0)

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
          <Amount amount={amount} size="title" tone={account.anchored ? 'auto' : 'muted'} />
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
      </div>

      <Facts account={account} />
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
