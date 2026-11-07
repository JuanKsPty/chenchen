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
 * contribution. It always comes off what is free: it reaches its account as a
 * transfer, which is never spending.
 *
 * Under its name, where the month stands, from what reached its account as transfers
 * (savingStatus): "Este mes apartaste $150.00 de $200.00", "Ya apartaste los $200.00
 * de este mes", "Aún no llega nada este mes", "No llegó nada en octubre"; with no
 * account, "Sin cuenta: solo cuenta lo que planeas". Next month, where it goes.
 *
 * With an account, what the account holds, as Cuentas shows it: "$3,450.00 en
 * Inversión". With a goal, against it, with a track in the accent like any plan
 * figure ("$3,450.00 de $10,000.00 en Inversión"), and, with a date, roughly what each
 * month to it would take: "Para marzo te faltan unos $550 al mes". Never red, never
 * a warning: a goal is not a debt. Past the goal the track stays full rather than
 * drawing the ink that marks spending past a cap.
 *
 * Under them, the suggestions, each with "Agregar" and "No" (SuggestionList).
 */
import { PiggyBankIcon } from 'lucide-react'
import { useRef } from 'react'

import type { Account, MonthPlan, PlanSavingLine, PlanSuggestion } from '@/api/types'
import { Amount, PlanTrack, SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { shortAccountLabel } from '@/lib/account'
import { PLAN_SECTIONS } from '@/lib/nav'
import { PLAN_LIST, PlanRow } from './PlanRow'
import { SuggestionList } from './Suggestions'
import { goalNeeded, monthWord, savingStatus } from './words'

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
              const kept = account
                ? account.alias?.trim() || shortAccountLabel(account)
                : undefined
              return (
                <li key={line.id}>
                  <PlanRow
                    icon={PiggyBankIcon}
                    title={line.name}
                    subtitle={savingStatus(
                      line,
                      plan,
                      kept ? `Al mes, en ${kept}` : 'Al mes',
                    )}
                    amount={<Amount amount={line.amount} />}
                    onClick={onEdit && (() => onEdit(line))}
                    footer={
                      kept && line.progress ? (
                        <Progress line={line} progress={line.progress.cents} kept={kept} />
                      ) : undefined
                    }
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

/**
 * What the saving's account holds (`progress`, as Cuentas shows it), against its goal
 * when it has one, and what each month to the goal's date would take.
 */
function Progress({
  line,
  progress,
  kept,
}: {
  line: PlanSavingLine
  progress: number
  kept: string
}) {
  const target = line.target?.cents
  const needed = goalNeeded(line)
  const caption = 'text-small font-medium text-pretty text-muted-foreground'
  if (!target) {
    return (
      <span className={caption}>
        <Amount amount={progress} size="inline" /> en {kept}
      </span>
    )
  }
  return (
    <>
      <PlanTrack amount={Math.min(progress, target)} cap={target} size="sm" />
      <span className={caption}>
        <Amount amount={progress} size="inline" /> de{' '}
        <Amount amount={target} size="inline" /> en {kept}
        {progress >= target && ' · llegaste a la meta'}
      </span>
      {needed && <span className={caption}>{needed}</span>}
    </>
  )
}
