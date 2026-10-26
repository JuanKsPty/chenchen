/**
 * The brokerage account's own pieces.
 *
 *   <InvestmentSync account />   "Sincronizar ahora" and "Cambiar conexión", then
 *                                what went wrong (the last sync failing, this one
 *                                failing) or what to look at after it worked.
 *   <PositionsCard account />    the holdings: Símbolo, Cant., Precio, Valor, as of
 *                                the last sync.
 *   <TradesCard account />       the last 20 trades, static rows.
 *
 * A trade is never red: buying is money leaving, drawn like any outflow.
 */
import { RefreshCwIcon, TrendingUpIcon } from 'lucide-react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { Account, InvestmentSyncResult, InvestmentTrade } from '@/api/types'
import { LinkIBKRDialog } from '@/components/LinkIBKRDialog'
import { Amount, Notice, SectionCard, Tile } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { formatRange } from '@/lib/format'
import {
  useInvestmentLink,
  useInvestmentTrades,
  usePortfolio,
  useSyncInvestmentAccount,
} from '@/lib/queries'
import { cn } from '@/lib/utils'
import { LOAD_FAILED as GENERIC_ERROR, shortDate } from './bank'

const quantity = new Intl.NumberFormat('es-PA', { maximumFractionDigits: 4 })

function plural(n: number, one: string, many: string) {
  return n === 1 ? `1 ${one}` : `${n} ${many}`
}

/**
 * What a sync did, for its toast: what came in on the first line, then, when the
 * report says so, the period it covers, what was already there and IBKR's own
 * cash. "Listo: 2 operaciones nuevas y 3 movimientos nuevos." / "Del 01–23 oct. 5
 * ya estaban y no los repetimos. Efectivo según IBKR: $2,477.40."
 */
function syncToast(outcome: InvestmentSyncResult) {
  const title =
    outcome.trades_new + outcome.cash_new === 0
      ? 'Listo: no había nada nuevo.'
      : `Listo: ${plural(outcome.trades_new, 'operación nueva', 'operaciones nuevas')} y ${plural(outcome.cash_new, 'movimiento nuevo', 'movimientos nuevos')}.`
  const repeated = outcome.trades_duplicate + outcome.cash_duplicate
  const period =
    outcome.period_from && outcome.period_to
      ? `Del ${formatRange(outcome.period_from, outcome.period_to)}.`
      : null
  const details = [
    period,
    repeated === 1 ? '1 ya estaba y no lo repetimos.' : null,
    repeated > 1 ? `${repeated} ya estaban y no los repetimos.` : null,
  ].filter(Boolean)
  const cash = outcome.reported_cash
  if (details.length === 0 && !cash) return toast(title)
  return toast(title, {
    description: (
      <>
        {details.join(' ')}
        {cash && (
          <>
            {details.length > 0 && ' '}
            Efectivo según IBKR: <Amount amount={cash} size="inline" />.
          </>
        )}
      </>
    ),
  })
}

export function InvestmentSync({ account }: { account: Account }) {
  const link = useInvestmentLink(account.id)
  const sync = useSyncInvestmentAccount(account.id)
  const result = sync.data

  const run = () => sync.mutate(undefined, { onSuccess: syncToast })

  if (!link.data) return null

  const lastFailed =
    link.data.last_sync_status === 'error' && !sync.isPending && !sync.isError

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" onClick={run} disabled={sync.isPending}>
          {sync.isPending ? <Spinner /> : <RefreshCwIcon aria-hidden="true" />}
          {sync.isPending ? 'Sincronizando…' : 'Sincronizar ahora'}
        </Button>
        <LinkIBKRDialog
          accountId={account.id}
          trigger={<Button variant="ghost">Cambiar conexión</Button>}
        />
      </div>

      {sync.isError && (
        <Notice
          className="lg:max-w-170"
          title="No pudimos sincronizar con Interactive Brokers"
          description={
            sync.error instanceof ApiError && sync.error.status < 500
              ? sync.error.message
              : GENERIC_ERROR
          }
          action={{ label: 'Intentar otra vez', onClick: run }}
        />
      )}
      {lastFailed && (
        <Notice
          className="lg:max-w-170"
          title="La última sincronización falló"
          description={link.data.last_sync_error ?? GENERIC_ERROR}
          action={{ label: 'Intentar otra vez', onClick: run }}
        />
      )}
      {result && result.warnings.length > 0 && (
        <Notice
          className="lg:max-w-170"
          title="Sincronizamos, pero revisa esto"
          description={result.warnings.join(' ')}
        />
      )}
    </div>
  )
}

export function PositionsCard({
  account,
  className,
}: {
  account: Account
  className?: string
}) {
  const portfolio = usePortfolio(account.id)
  const positions = portfolio.data?.positions ?? []
  const asOf = positions[0]?.as_of

  return (
    <SectionCard title="Posiciones" layout="list" className={className}>
      {portfolio.isLoading ? (
        <Skeleton className="mx-3 mb-3 h-40" />
      ) : portfolio.isError ? (
        <p className="px-3 pb-3 text-body text-pretty text-muted-foreground md:px-4">
          {GENERIC_ERROR}
        </p>
      ) : positions.length === 0 ? (
        <p className="px-3 pb-3 text-body text-muted-foreground md:px-4">
          Todavía no hay posiciones. Sincroniza para traerlas.
        </p>
      ) : (
        <>
          <table className="w-full">
            <thead>
              <tr className="text-small font-semibold text-muted-foreground">
                <th scope="col" className="py-2 pr-2 pl-3 text-left font-semibold md:pl-4">
                  Símbolo
                </th>
                {/* Below sm, quantity and price join the symbol's cell as a second
                    line ("75 a $520.00", like Operaciones), so four columns never
                    push the table past a 320px screen. */}
                <th
                  scope="col"
                  className="hidden px-2 py-2 text-right font-semibold sm:table-cell"
                >
                  Cant.
                </th>
                <th
                  scope="col"
                  className="hidden px-2 py-2 text-right font-semibold sm:table-cell"
                >
                  Precio
                </th>
                <th scope="col" className="py-2 pr-3 pl-2 text-right font-semibold md:pr-4">
                  Valor
                </th>
              </tr>
            </thead>
            <tbody>
              {positions.map((position) => (
                <tr key={position.symbol} className="text-body">
                  <th
                    scope="row"
                    className="py-3 pr-2 pl-3 text-left font-semibold md:py-2.5 md:pl-4"
                  >
                    {position.symbol}
                    <span className="block text-small font-medium text-muted-foreground sm:hidden">
                      {quantity.format(position.quantity)}
                      {position.mark_price && (
                        <>
                          {' '}
                          a <Amount amount={position.mark_price} size="inline" />
                        </>
                      )}
                    </span>
                  </th>
                  <td className="hidden px-2 py-3 text-right font-medium tabular-nums sm:table-cell md:py-2.5">
                    {quantity.format(position.quantity)}
                  </td>
                  <td className="hidden px-2 py-3 text-right font-medium sm:table-cell md:py-2.5">
                    {position.mark_price && (
                      <Amount amount={position.mark_price} size="inline" />
                    )}
                  </td>
                  <td className="py-3 pr-3 pl-2 text-right md:py-2.5 md:pr-4">
                    {position.market_value && <Amount amount={position.market_value} />}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {asOf && (
            <p className="px-3 pt-1 pb-3 text-small font-medium text-muted-foreground md:px-4">
              Al {shortDate(asOf)}, según Interactive Brokers.
            </p>
          )}
        </>
      )}
    </SectionCard>
  )
}

export function TradesCard({
  account,
  className,
}: {
  account: Account
  className?: string
}) {
  const trades = useInvestmentTrades(account.id)
  const list = trades.data?.trades ?? []

  return (
    <SectionCard title="Operaciones" layout="list" className={className}>
      {trades.isLoading ? (
        <Skeleton className="mx-3 mb-3 h-40" />
      ) : trades.isError ? (
        <p className="px-3 pb-3 text-body text-pretty text-muted-foreground md:px-4">
          {GENERIC_ERROR}
        </p>
      ) : list.length === 0 ? (
        <p className="px-3 pb-3 text-body text-muted-foreground md:px-4">
          Todavía no hay operaciones.
        </p>
      ) : (
        <ul className="flex flex-col">
          {list.map((trade) => (
            <li key={trade.id}>
              <TradeRow trade={trade} />
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  )
}

function TradeRow({ trade, className }: { trade: InvestmentTrade; className?: string }) {
  return (
    <div
      className={cn(
        'grid min-h-16 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 md:min-h-14 md:py-2',
        className,
      )}
    >
      <Tile icon={TrendingUpIcon} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-body font-medium">
          {trade.side === 'buy' ? 'Compra' : 'Venta'} · {trade.symbol}
        </span>
        <span className="truncate text-small font-medium text-muted-foreground">
          {quantity.format(trade.quantity)} a <Amount amount={trade.price} size="inline" />{' '}
          · {shortDate(trade.trade_date)}
        </span>
      </span>
      <Amount amount={trade.net_cash} signed className="text-right" />
    </div>
  )
}
