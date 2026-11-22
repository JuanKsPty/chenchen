/**
 * <BalanceTrendCard account className? />
 *
 * One account month by month, on its page: the balance at the end of each of the last
 * 12 months and today (useNetWorthHistory with the account). A card reads as its debt,
 * "Deuda mes a mes", so a line going up is more owed; every other account as its
 * balance, a broker's with its positions. Nothing shows until there are two points.
 * A card someone else pays has no balance of the owner's, so it never asks.
 */
import type { Account } from '@/api/types'
import { SectionCard, TrendChart } from '@/components/kit'
import { paidByOther } from '@/lib/account'
import { useNetWorthHistory } from '@/lib/queries'

export function BalanceTrendCard({
  account,
  className,
}: {
  account: Account
  className?: string
}) {
  if (paidByOther(account)) return null
  return <Trend account={account} className={className} />
}

function Trend({ account, className }: { account: Account; className?: string }) {
  const history = useNetWorthHistory({ months: 12, account: account.id })
  const points = history.data?.points ?? []
  if (points.length < 2) return null

  const debt = account.class === 'liability'
  const title = debt ? 'Deuda mes a mes' : 'Saldo mes a mes'
  return (
    <SectionCard title={title} className={className}>
      <TrendChart
        label={title}
        points={points.map((p) => ({
          day: p.day,
          cents: debt ? p.owed.cents : p.total.cents,
          partial: p.partial,
        }))}
      />
    </SectionCard>
  )
}
