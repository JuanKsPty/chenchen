/**
 * <NetWorthCard accounts? netWorth? className? />
 *
 *   accounts  the account list (useAccounts().data?.accounts), for the breakdown
 *   netWorth  the API's net worth (useAccounts().data?.net_worth), for the figure
 *
 * "Patrimonio neto": the app's one solid veranera block (spec card/surface). The
 * figure in display, then Tienes · Debes · Inversiones in three columns. The
 * breakdown is the three account groups' totals (lib/account groupAccounts), so it
 * reads like Cuentas: Tienes is the bank accounts, Inversiones is a broker's cash
 * plus its positions. Tienes − Debes + Inversiones is the API's total.
 *
 * Without data yet it keeps its shape with placeholders. From lg it is at least
 * 280px tall, the figure on top and the breakdown at the bottom.
 */
import { useId, useMemo } from 'react'

import type { Account, Amount as AmountValue, NetWorth } from '@/api/types'
import { Amount } from '@/components/kit'
import { Skeleton } from '@/components/ui/skeleton'
import { amountOf, groupAccounts } from '@/lib/account'
import { cn } from '@/lib/utils'

export function NetWorthCard({
  accounts,
  netWorth,
  className,
}: {
  accounts?: Account[]
  netWorth?: NetWorth
  className?: string
}) {
  const headingId = useId()

  const breakdown = useMemo(() => {
    if (!accounts) return null
    const totals = new Map(groupAccounts(accounts).map((group) => [group.key, group.total]))
    const zero = amountOf(0)
    return [
      { label: 'Tienes', amount: totals.get('cuentas') ?? zero },
      { label: 'Debes', amount: totals.get('tarjetas') ?? zero },
      { label: 'Inversiones', amount: totals.get('inversiones') ?? zero },
    ] satisfies { label: string; amount: AmountValue }[]
  }, [accounts])

  return (
    <section
      aria-labelledby={headingId}
      aria-busy={!netWorth || undefined}
      className={cn(
        'flex min-w-0 flex-col justify-between gap-4 rounded-2xl bg-primary p-4 text-primary-foreground md:p-6 lg:min-h-70 lg:gap-8',
        className,
      )}
    >
      <div className="flex flex-col gap-1 lg:gap-2">
        <h2 id={headingId} className="text-body font-semibold">
          Patrimonio neto
        </h2>
        {netWorth ? (
          <p>
            <Amount amount={netWorth.total} size="display" />
          </p>
        ) : (
          <Placeholder className="h-13 w-56" />
        )}
      </div>

      {/* Three equal columns. Each is at least as wide as its amount, so a figure
          wider than a third (a 320px phone, a seven-figure balance) sends
          Inversiones to its own line instead of running into its neighbour or
          past the card's edge. The 1px keeps rounding from wrapping a row that fits. */}
      <dl className="flex flex-wrap gap-4 lg:gap-6">
        {(breakdown ?? PLACEHOLDER_LABELS.map((label) => ({ label, amount: null }))).map(
          ({ label, amount }) => (
            <div
              key={label}
              className="flex grow basis-[calc((100%-2rem)/3-1px)] flex-col lg:basis-[calc((100%-3rem)/3-1px)]"
            >
              <dt className="text-small font-medium">{label}</dt>
              <dd>
                {amount ? (
                  <Amount amount={amount} />
                ) : (
                  <Placeholder className="my-1 h-4 w-20" />
                )}
              </dd>
            </div>
          ),
        )}
      </dl>
    </section>
  )
}

const PLACEHOLDER_LABELS = ['Tienes', 'Debes', 'Inversiones'] as const

/** A skeleton that reads on the veranera ground: white at low strength. */
function Placeholder({ className }: { className?: string }) {
  return (
    <Skeleton aria-hidden="true" className={cn('bg-primary-foreground/20', className)} />
  )
}
