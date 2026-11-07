/**
 * <PlanHero plan onEditIncome? onOpenIncome suggestions? headingRef? />
 *
 *   onEditIncome  "Editar ingreso", a ghost button; left out for a month that can no
 *                 longer be edited
 *   onOpenIncome  a paycheck tapped, with the element that opened it: its sheet,
 *                 which says which deposit it was
 *   suggestions   paychecks the history suggests planning, offered as "¿También es
 *                 un sueldo?"; left out where they are not offered
 *   headingRef    the card's heading, for focus to land on when the plan was just
 *                 set up and what opened the setup is gone
 *
 * The plan's headline card, OutflowCard's shape. On the left, from the top:
 *   - "Te quedan" and the view's one display figure: what is left of what is free.
 *     The label changes rather than the figure going below zero: "Por encima de lo
 *     libre" and how far, in ink and unsigned; "Te sobraron" for a month that closed;
 *     "Libre para gastar" for next month, or when the plan leaves nothing free.
 *   - the track: what went out against what was free (PlanTrack)
 *   - "Llevas $35.63 de $580.00 libres." and "24 días · unos $22 al día"
 *   - the two quincenas, each with its share of what is free
 * On the right (under it on a narrow card), the account it comes from, as a <dl>:
 * Ingreso esperado ("Entraron $650.00", then each paycheck and where it stands:
 * "Sueldo · Entró el 16 nov", "Sueldo · Llega el 30 nov"), Pagos fijos, Ahorro,
 * Compras planeadas when the month sets any aside, Libre para gastar, and "Editar
 * ingreso". Income that came in unplanned is said
 * there (useAddToFree): when a paycheck still waiting may be that money, "que no
 * reconocimos" and the way to say which it was; otherwise "que no estaban en tu
 * plan", with "Sumar a lo libre" when that would count it exactly once. It never
 * changes what is free on its own. Then the paychecks the history suggests that the
 * plan lacks, each with "Agregar" and "No" (SuggestionList). Under both columns,
 * what "Llevas" counts.
 *
 * Not a live region: a refetch would read it all out again. Its figures change after
 * an edit, which the edit's toast already says.
 */
import { useId, useRef, type ReactNode, type RefObject } from 'react'

import type { MonthPlan, PlanIncomeLine, PlanQuincena, PlanSuggestion } from '@/api/types'
import { Amount, PlanCaption, PlanTrack } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { formatCivilDayMonth, formatRange } from '@/lib/format'
import { cn } from '@/lib/utils'
import { SuggestionList } from './Suggestions'
import { useAddToFree } from './useAddToFree'
import {
  daysLine,
  headline,
  incomeStatus,
  monthStart,
  monthWord,
  shortfall,
  spentLine,
} from './words'

/** A paycheck tapped, and the element that opened its sheet, for focus to go back to. */
export type OpenIncome = (line: PlanIncomeLine, opener: HTMLElement) => void

export function PlanHero({
  plan,
  onEditIncome,
  onOpenIncome,
  suggestions,
  headingRef,
  className,
}: {
  plan: MonthPlan
  onEditIncome?: () => void
  onOpenIncome: OpenIncome
  suggestions?: PlanSuggestion[]
  headingRef?: RefObject<HTMLHeadingElement | null>
  className?: string
}) {
  const headingId = useId()
  const { summary } = plan
  const head = headline(plan)
  const nothingFree = summary.free.cents <= 0
  const future = plan.state === 'future'

  let firstLine: ReactNode
  let secondLine: string | null
  if (nothingFree) {
    firstLine = shortfall(plan)
    secondLine = null
  } else if (future) {
    firstLine = `Empieza el ${monthStart(plan.month)}.`
    secondLine = daysLine(plan)
  } else {
    firstLine = spentLine(plan)
    secondLine = daysLine(plan)
  }

  return (
    <section
      aria-labelledby={headingId}
      className={cn(
        '@container flex flex-col gap-6 rounded-2xl bg-card p-4 text-card-foreground shadow-card md:p-6',
        className,
      )}
    >
      <div className="grid gap-6 @lg:grid-cols-[minmax(0,1fr)_minmax(0,16rem)] @lg:gap-8">
        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex flex-col gap-2">
            <h2
              ref={headingRef}
              id={headingId}
              tabIndex={-1}
              className="text-body font-semibold outline-none"
            >
              {head.label}
            </h2>
            <p>
              <Amount amount={head.figure} size="display" />
            </p>
          </div>
          {head.track && <PlanTrack amount={summary.spent} cap={summary.free} />}
          <p className="text-small font-medium text-pretty text-muted-foreground">
            {firstLine}
          </p>
          {secondLine && <p className="text-body font-semibold">{secondLine}</p>}
          {!nothingFree && <Quincenas plan={plan} />}
        </div>

        <Equation
          plan={plan}
          onEditIncome={onEditIncome}
          onOpenIncome={onOpenIncome}
          suggestions={suggestions}
        />
      </div>

      {plan.state !== 'future' && plan.planned && (
        <p className="text-small font-medium text-pretty text-muted-foreground">
          {plan.state === 'past'
            ? `Usaste: lo que salió en ${monthWord(plan.month)}, sin pagos fijos ni compras planeadas.`
            : 'Llevas: lo que salió este mes, sin pagos fijos ni compras planeadas.'}
        </p>
      )}
    </section>
  )
}

/**
 * The two halves of the month, each with its share of what is free (by days, the
 * server's split) and what went out of it. A half still to come says when it starts;
 * there is no carry-over between them, the month's line is the one that counts.
 */
function Quincenas({ plan }: { plan: MonthPlan }) {
  return (
    <dl className="mt-2 grid grid-cols-2 gap-4">
      {plan.quincenas.map((q, i) => (
        <Quincena
          key={q.from}
          quincena={q}
          title={i === 0 ? 'Primera quincena' : 'Segunda quincena'}
          started={
            plan.state === 'past' || (plan.state === 'current' && plan.today >= q.from)
          }
        />
      ))}
    </dl>
  )
}

function Quincena({
  quincena: q,
  title,
  started,
}: {
  quincena: PlanQuincena
  title: string
  started: boolean
}) {
  const income = q.income_expected.cents
  const fixed = q.fixed_due.cents
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <dt className="flex flex-col">
        <span className="text-small font-semibold">{title}</span>
        <span className="text-small font-medium text-muted-foreground">
          {formatRange(q.from, q.to)}
        </span>
      </dt>
      <dd className="flex flex-col gap-1.5">
        {started ? (
          <PlanCaption amount={q.spent} cap={q.free} of="libres" />
        ) : (
          <span className="text-small font-medium text-pretty text-muted-foreground">
            <Amount amount={q.free} size="inline" /> libres · empieza el{' '}
            {formatCivilDayMonth(q.from)}
          </span>
        )}
        <PlanTrack amount={started ? q.spent : 0} cap={q.free} size="sm" />
        <span className="text-small font-medium text-pretty text-muted-foreground">
          {income > 0 ? (
            <>
              Entran <Amount amount={income} size="inline" />
            </>
          ) : (
            'Sin ingreso esperado'
          )}
          {fixed > 0 && (
            <>
              {' '}
              · pagos <Amount amount={fixed} size="inline" />
            </>
          )}
        </span>
      </dd>
    </div>
  )
}

/**
 * Where what is free comes from: Ingreso esperado − Pagos fijos − Ahorro − Compras
 * planeadas, the last only when the month sets something aside for one (or bought
 * one beyond what was set aside): a line of $0.00 would only be noise.
 */
function Equation({
  plan,
  onEditIncome,
  onOpenIncome,
  suggestions,
}: {
  plan: MonthPlan
  onEditIncome?: () => void
  onOpenIncome: OpenIncome
  suggestions?: PlanSuggestion[]
}) {
  // Where focus goes when the last suggestion is answered and its list leaves.
  const editRef = useRef<HTMLButtonElement>(null)
  const { summary, income } = plan
  const extra = useAddToFree(plan)
  const seen = plan.state !== 'future'

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <dl className="flex flex-col gap-2 text-body">
        <Term label="Ingreso esperado" amount={summary.income.cents}>
          {seen && (
            <dd className="col-span-2 text-small font-medium text-muted-foreground">
              Entraron <Amount amount={income.actual} size="inline" />
            </dd>
          )}
          {seen && income.items.length > 0 && (
            <Paychecks plan={plan} onOpen={onOpenIncome} />
          )}
        </Term>
        <Term label="Pagos fijos" amount={-summary.fixed.cents} />
        <Term label="Ahorro" amount={-summary.savings.cents} />
        {summary.purchases.cents > 0 && (
          <Term label="Compras planeadas" amount={-summary.purchases.cents} />
        )}
        <Term label="Libre para gastar" amount={Math.max(0, summary.free.cents)} strong />
      </dl>

      {extra.amount > 0 && (
        <div className="flex flex-col items-start gap-1">
          <p className="text-small font-medium text-pretty text-muted-foreground">
            Entraron <Amount amount={extra.amount} size="inline" />{' '}
            {extra.paycheck
              ? `que no reconocimos. ¿Es ${extra.paycheck.name}?`
              : 'que no estaban en tu plan.'}
          </p>
          {extra.paycheck ? (
            <Button
              variant="ghost"
              className="-ml-3"
              onClick={(event) => {
                if (extra.paycheck) onOpenIncome(extra.paycheck, event.currentTarget)
              }}
            >
              Elegir cuál fue
            </Button>
          ) : (
            extra.addable && (
              <Button
                variant="ghost"
                className="-ml-3"
                aria-disabled={extra.busy || undefined}
                onClick={() => void extra.run()}
              >
                {extra.busy && <Spinner aria-hidden="true" />}
                Sumar a lo libre
              </Button>
            )
          )}
        </div>
      )}

      {suggestions && suggestions.length > 0 && (
        <SuggestionList
          plan={plan}
          title="¿También es un sueldo?"
          suggestions={suggestions}
          fallback={editRef}
        />
      )}

      {onEditIncome && (
        <Button
          ref={editRef}
          variant="ghost"
          className="-ml-3 self-start"
          onClick={onEditIncome}
        >
          Editar ingreso
        </Button>
      )}
    </div>
  )
}

/**
 * Each paycheck of the month and where it stands, so two of $650 each say on their
 * own day whether they came in: each a 44px button to its sheet.
 */
function Paychecks({ plan, onOpen }: { plan: MonthPlan; onOpen: OpenIncome }) {
  return (
    <dd className="col-span-2">
      <ul className="-mx-3 flex flex-col">
        {plan.income.items.map((line) => (
          <li key={line.id}>
            <button
              type="button"
              onClick={(event) => onOpen(line, event.currentTarget)}
              className="flex min-h-11 w-full cursor-pointer items-center rounded-2xl px-3 py-1.5 text-left text-small font-medium text-pretty text-muted-foreground transition-colors duration-180 ease-out hover:bg-muted active:bg-muted"
            >
              <span className="min-w-0">
                <span className="font-semibold text-foreground">{line.name}</span>
                {` · ${incomeStatus(line, plan)}`}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </dd>
  )
}

/**
 * One line of the account: its name and its amount, a payment with its minus.
 * `children` are more <dd>s under it, across both columns.
 */
function Term({
  label,
  amount,
  strong = false,
  children,
}: {
  label: string
  amount: number
  strong?: boolean
  children?: ReactNode
}) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-3">
      <dt className={cn(strong ? 'font-semibold' : 'font-medium')}>{label}</dt>
      <dd>
        <Amount amount={amount} className={strong ? undefined : 'font-medium'} />
      </dd>
      {children}
    </div>
  )
}
