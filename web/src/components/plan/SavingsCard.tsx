/**
 * <SavingsCard plan accounts onAdd? onEdit? suggestions? className? />
 *
 *   accounts     the owner's accounts, to name where each saving is kept
 *   onAdd        "Agregar ahorro", the card's ghost action; left out when the month
 *                can no longer be edited
 *   onEdit       tapping a saving opens it; left out likewise
 *   suggestions  accounts the history shows money set aside in every month, offered
 *                as "¿También es ahorro?"; left out where they are not offered
 *
 * "Ahorro", #ahorro: what the month sets aside, one row per saving with its monthly
 * contribution and the account it goes to. It always comes off what is free: it
 * reaches its account as a transfer, which is never spending. How far a goal has
 * got comes later. Under them, the suggestions, each with "Agregar" and "No"
 * (SuggestionList).
 */
import { PiggyBankIcon } from 'lucide-react'
import { useRef } from 'react'

import type { Account, MonthPlan, PlanSavingLine, PlanSuggestion } from '@/api/types'
import { Amount, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { shortAccountLabel } from '@/lib/account'
import { PLAN_SECTIONS } from '@/lib/nav'
import { PLAN_LIST, PlanRow } from './PlanRow'
import { SuggestionList } from './Suggestions'
import { monthWord } from './words'

export function SavingsCard({
  plan,
  accounts,
  onAdd,
  onEdit,
  suggestions,
  className,
}: {
  plan: MonthPlan
  accounts: Account[] | undefined
  onAdd?: () => void
  onEdit?: (line: PlanSavingLine) => void
  suggestions?: PlanSuggestion[]
  className?: string
}) {
  // Where focus goes when the last suggestion is answered and its list leaves.
  const addRef = useRef<HTMLButtonElement>(null)
  const byId = new Map((accounts ?? []).map((a) => [a.id, a]))

  return (
    <SectionCard
      id={PLAN_SECTIONS.savings}
      title="Ahorro"
      className={className}
      action={
        onAdd && (
          <Button ref={addRef} variant="ghost" onClick={onAdd}>
            Agregar ahorro
          </Button>
        )
      }
    >
      <div className="flex flex-col gap-4">
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

        {suggestions && suggestions.length > 0 && (
          <SuggestionList
            plan={plan}
            title="¿También es ahorro?"
            suggestions={suggestions}
            fallback={addRef}
          />
        )}
      </div>
    </SectionCard>
  )
}
