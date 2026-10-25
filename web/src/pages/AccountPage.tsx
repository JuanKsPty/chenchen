/**
 * /cuentas/:id — one account (spec C-Cuenta-Desktop and C-Cuenta-Mobile).
 *
 * Top: the balance and whether it matches the bank (AccountOverview). Then, from lg on
 * a 12-column grid:
 *   - a bank account or a card: Movimientos (7) beside "Saldos del banco" (5);
 *   - a brokerage account: Posiciones (7) over Movimientos, Operaciones (5) beside.
 * Below lg it is one column: balance, notice, movements (and a broker's positions and
 * trades before its movements).
 *
 * The overflow menu is the ellipsis by the desktop title and the mobile bar's "Más
 * opciones" (useTopBar). "Revisar", "Ver saldos del banco" and "Ver detalle" open the
 * review sheet by setting `?revisar=1`, which the sheet clears again when it closes.
 */
import { useSearchParams, useParams } from 'react-router-dom'

import { ApiError } from '@/api/client'
import type { Account } from '@/api/types'
import { AccountMovements } from '@/components/account/AccountMovements'
import { AccountOverview } from '@/components/account/AccountOverview'
import { useAccountActions } from '@/components/account/AccountActions'
import { BankFiguresCard, BankFiguresSkeleton } from '@/components/account/BankFiguresCard'
import { PositionsCard, TradesCard } from '@/components/account/Investments'
import { ReviewSheet } from '@/components/account/ReviewSheet'
import { EmptyState, MovementList } from '@/components/kit'
import { DESKTOP_QUERY, useMediaQuery } from '@/components/shell/media'
import { useTopBar } from '@/components/shell/TopBar'
import { Skeleton } from '@/components/ui/skeleton'
import { shortAccountLabel } from '@/lib/account'
import { ACCOUNTS_PATH, isReviewRequested } from '@/lib/nav'
import { useAccount } from '@/lib/queries'
import { useDocumentTitle } from '@/lib/title'

export function AccountPage() {
  const { id = '' } = useParams()
  const account = useAccount(id)
  useDocumentTitle(account.data ? shortAccountLabel(account.data) : 'Cuentas')

  const missing = account.error instanceof ApiError && account.error.status === 404
  // A refresh that fails for any other reason keeps the figures already on screen.
  if (account.isError && (missing || !account.data)) {
    return (
      <EmptyState
        headingLevel={1}
        title={missing ? 'No encontramos esta cuenta' : 'No pudimos abrir esta cuenta'}
        description={
          missing
            ? 'Puede que la hayas eliminado.'
            : 'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'
        }
        action={{ label: 'Ver tus cuentas', to: ACCOUNTS_PATH }}
      />
    )
  }

  if (!account.data) return <AccountSkeleton />

  return <AccountDetail key={account.data.id} account={account.data} />
}

function AccountDetail({ account }: { account: Account }) {
  const [params, setParams] = useSearchParams()
  const { items, dialogs, openCheckpoint } = useAccountActions(account)
  const brokerage = account.type === 'brokerage'
  // The bank figures card is desktop only; below lg it would only cost a request.
  const desktop = useMediaQuery(DESKTOP_QUERY)

  useTopBar({ title: shortAccountLabel(account), back: ACCOUNTS_PATH, menu: items })

  const setReview = (open: boolean) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        if (open) next.set('revisar', '1')
        else next.delete('revisar')
        return next
      },
      { replace: true },
    )

  return (
    // Below lg: 8 (shell) + 16 under the bar, the artboard's 24.
    <div className="flex flex-col gap-6 max-lg:pt-4 lg:gap-10">
      <AccountOverview
        account={account}
        menu={items}
        onReview={() => setReview(true)}
        onAnchor={() => openCheckpoint(true)}
      />

      {brokerage ? (
        <div className="grid gap-4 lg:gap-6 lg:grid-cols-12 lg:grid-rows-[auto_1fr] lg:items-start">
          <PositionsCard account={account} className="lg:col-span-7" />
          <TradesCard
            account={account}
            className="lg:col-span-5 lg:col-start-8 lg:row-span-2 lg:row-start-1"
          />
          <AccountMovements account={account} className="lg:col-span-7" />
        </div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-12 lg:items-start lg:gap-6">
          <AccountMovements account={account} className="lg:col-span-7" />
          {desktop && (
            <BankFiguresCard
              account={account}
              onReview={() => setReview(true)}
              className="lg:col-span-5"
            />
          )}
        </div>
      )}

      <ReviewSheet
        account={account}
        open={isReviewRequested(params)}
        onOpenChange={setReview}
      />
      {dialogs}
    </div>
  )
}

/**
 * Shaped like the page it stands in for, as the other pages' loading states are:
 * the title and the balance, then the movements card with its rows (the heading
 * above the card below lg, inside it from lg) and, from lg, the side card.
 */
function AccountSkeleton() {
  return (
    <div aria-busy="true" className="flex flex-col gap-6 max-lg:pt-4 lg:gap-10">
      <span role="status" className="sr-only">
        Abriendo la cuenta…
      </span>
      <div className="flex flex-col gap-6">
        <div className="flex items-center gap-4 max-lg:hidden">
          <Skeleton className="size-14 rounded-full" />
          <div className="flex flex-col gap-2">
            <Skeleton className="h-8 w-56 rounded-full" />
            <Skeleton className="h-4 w-40 rounded-full" />
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Skeleton className="h-4 w-24 rounded-full" />
          <Skeleton className="h-9 w-48 rounded-full" />
        </div>
      </div>
      <div className="grid gap-4 lg:grid-cols-12 lg:items-start lg:gap-6">
        <div className="flex min-w-0 flex-col gap-3 lg:col-span-7 lg:gap-0 lg:rounded-2xl lg:bg-card lg:p-2 lg:shadow-card">
          <div className="box-content flex min-h-11 items-center pl-1 lg:pt-4 lg:pb-2 lg:pl-4">
            <Skeleton className="h-6 w-36 rounded-full" />
          </div>
          <div className="rounded-2xl bg-card p-1 shadow-card lg:bg-transparent lg:p-0 lg:shadow-none">
            <MovementList entries={[]} loading />
          </div>
        </div>
        <BankFiguresSkeleton className="max-lg:hidden lg:col-span-5" />
      </div>
    </div>
  )
}
