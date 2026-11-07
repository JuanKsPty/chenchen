/**
 * <SavingsCard plan accounts onAdd? onEdit? className? />
 *
 *   accounts  the owner's accounts, to name where each saving is kept
 *   onAdd     "Agregar ahorro", the card's ghost action; left out when the month can
 *             no longer be edited
 *   onEdit    tapping a saving opens it; left out likewise
 *
 * "Ahorro", #ahorro: what the month sets aside, one row per saving with its monthly
 * contribution and the account it goes to. It always comes off what is free: it
 * reaches its account as a transfer, which is never spending. How far a goal has
 * got comes later.
 */
import { PiggyBankIcon } from 'lucide-react'

import type { Account, MonthPlan, PlanSavingLine } from '@/api/types'
import { Amount, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { shortAccountLabel } from '@/lib/account'
import { PLAN_SECTIONS } from '@/lib/nav'
import { PLAN_LIST, PlanRow } from './PlanRow'
import { monthWord } from './words'

export function SavingsCard({
  plan,
  accounts,
  onAdd,
  onEdit,
  className,
}: {
  plan: MonthPlan
  accounts: Account[] | undefined
  onAdd?: () => void
  onEdit?: (line: PlanSavingLine) => void
  className?: string
}) {
  const byId = new Map((accounts ?? []).map((a) => [a.id, a]))

  return (
    <SectionCard
      id={PLAN_SECTIONS.savings}
      title="Ahorro"
      className={className}
      action={
        onAdd && (
          <Button variant="ghost" onClick={onAdd}>
            Agregar ahorro
          </Button>
        )
      }
    >
      {plan.savings.length === 0 ? (
        <p className="text-body text-pretty text-muted-foreground">
          {onAdd
            ? 'Separa algo cada mes, aunque sea poquito.'
            : `No apartabas nada en ${monthWord(plan.month)}.`}
        </p>
      ) : (
        <ul className={PLAN_LIST}>
          {plan.savings.map((line) => {
            const account = line.account_id ? byId.get(line.account_id) : undefined
            const where = account
              ? `Al mes, en ${account.alias?.trim() || shortAccountLabel(account)}`
              : 'Al mes'
            return (
              <li key={line.id}>
                <PlanRow
                  icon={PiggyBankIcon}
                  title={line.name}
                  subtitle={where}
                  amount={<Amount amount={line.amount} />}
                  onClick={onEdit && (() => onEdit(line))}
                />
              </li>
            )
          })}
        </ul>
      )}
    </SectionCard>
  )
}
