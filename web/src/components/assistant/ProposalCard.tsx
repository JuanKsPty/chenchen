/**
 * <ProposalCard proposal size onGone? />: one change the assistant proposed.
 *
 *   proposal  a Proposal (from the proposals query, or as the stream announced it)
 *   size      'panel' (desktop: 40px buttons) or 'sheet' (mobile: 44px)
 *   onGone    called after "Descartar" removes the card, so the panel can take
 *             the focus that was on its button
 *
 * Spec proposalCard: surface2, radius 16, padding 16, a column with 12 between
 * rows and no shadow. The type tag ("Regla", "Categoría"), the change in body
 * text with the bank's description in 600, then "Aplicar" and a muted ghost
 * "Descartar". "Aplicar" is secondary on the card's surface, not the spec's primary:
 * an answer can hold several proposals, and the view keeps one primary. Applying swaps the buttons for the green "Aplicado" chip inside a
 * polite live region and shows the "Aplicado." toast. There is no "Deshacer":
 * the server has no way to revert a proposal.
 *
 * The server writes each summary from the change itself; describeProposal reads
 * it back into the spec's wording ("Si el concepto contiene SUPER 99 →
 * Alimentación") and falls back to the summary as written.
 */
import { useRef, useState, type ReactNode } from 'react'
import { CheckIcon, Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { ApiError } from '@/api/client'
import type { Proposal } from '@/api/types'
import { Amount } from '@/components/kit'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatCivilDayMonth } from '@/lib/format'
import { useDecideProposal } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { useAssistantProposals } from './AssistantProvider'

const TAG: Record<Proposal['kind'], string> = {
  recategorize: 'Categoría',
  note: 'Nota',
  rule: 'Regla',
  transfer_decision: 'Transferencia',
  checkpoint: 'Saldo del banco',
}

const LABEL: Record<Proposal['kind'], string> = {
  recategorize: 'Propuesta de categoría',
  note: 'Propuesta de nota',
  rule: 'Propuesta de regla',
  transfer_decision: 'Propuesta de transferencia',
  checkpoint: 'Propuesta de saldo',
}

export function ProposalList({
  proposals,
  size,
  onGone,
}: {
  proposals: Proposal[]
  size: 'panel' | 'sheet'
  onGone?: () => void
}) {
  if (proposals.length === 0) return null
  return (
    <div className="flex flex-col gap-3">
      {proposals.map((proposal) => (
        <ProposalCard key={proposal.id} proposal={proposal} size={size} onGone={onGone} />
      ))}
    </div>
  )
}

export function ProposalCard({
  proposal,
  size,
  onGone,
}: {
  proposal: Proposal
  size: 'panel' | 'sheet'
  onGone?: () => void
}) {
  const decide = useDecideProposal()
  const { applied, markApplied, markDismissed } = useAssistantProposals()
  const [deciding, setDeciding] = useState<'apply' | 'reject' | null>(null)
  const ref = useRef<HTMLElement>(null)

  const isApplied = proposal.status === 'applied' || Boolean(applied[proposal.id])
  const { main, meta } = describeProposal(proposal)
  const buttonSize = size === 'sheet' ? 'default' : 'bar'

  const decideIt = (apply: boolean) => {
    if (decide.isPending) return
    setDeciding(apply ? 'apply' : 'reject')
    decide.mutate(
      { id: proposal.id, apply },
      {
        onSuccess: () => {
          if (apply) {
            markApplied(proposal)
            toast('Aplicado.')
            // The button that had focus is gone; the card keeps it in place.
            ref.current?.focus({ preventScroll: true })
          } else {
            markDismissed(proposal.id)
            onGone?.()
          }
        },
        onSettled: () => setDeciding(null),
      },
    )
  }

  return (
    <article
      ref={ref}
      tabIndex={-1}
      aria-label={LABEL[proposal.kind] ?? 'Propuesta'}
      className="flex flex-col items-start gap-3 rounded-2xl bg-muted p-4"
    >
      <Badge variant="label">{TAG[proposal.kind] ?? 'Propuesta'}</Badge>

      <div className="flex min-w-0 flex-col gap-1 self-stretch">
        <p className="text-body text-pretty wrap-break-word">{main}</p>
        {meta && <p className="text-small text-muted-foreground">{meta}</p>}
      </div>

      <div
        aria-live="polite"
        className={cn(
          'flex flex-wrap items-center gap-2',
          size === 'sheet' ? 'min-h-11' : 'min-h-10',
        )}
      >
        {isApplied ? (
          <Badge variant="positive">
            <CheckIcon aria-hidden="true" />
            Aplicado
          </Badge>
        ) : (
          <>
            {/* Secondary on the card's surface2, like "Confirmar" on a transfer
                pair: one answer can carry several proposals, and a view keeps a
                single primary (the bar's "Importar estado"). */}
            <Button
              variant="secondary"
              size={buttonSize}
              className="bg-card px-4 hover:bg-line has-[>svg:first-child]:pl-3"
              aria-disabled={decide.isPending || undefined}
              onClick={() => decideIt(true)}
            >
              {deciding === 'apply' && (
                <Loader2Icon aria-hidden="true" className="animate-spin" />
              )}
              Aplicar
            </Button>
            <Button
              variant="ghost"
              size={buttonSize}
              className="text-muted-foreground hover:bg-line"
              aria-disabled={decide.isPending || undefined}
              onClick={() => decideIt(false)}
            >
              Descartar
            </Button>
          </>
        )}
      </div>

      {decide.isError && (
        <p role="alert" className="text-small text-attention">
          {decide.error instanceof ApiError
            ? decide.error.message
            : 'No pudimos guardar tu decisión. Intenta otra vez.'}
        </p>
      )}
    </article>
  )
}

// ---------------------------------------------------------------------------
// Reading the server's summary back into the spec's wording
// ---------------------------------------------------------------------------

/** "2026-10-24 SUPER 99 VIA ESPAÑA -84.37", as the server describes a movement. */
const ENTRY = /^(\d{4}-\d{2}-\d{2}) (.+) (-?[\d,]+\.\d{2})$/
const ENTRY_WITH_REST = /^(\d{4}-\d{2}-\d{2}) (.+?) (-?[\d,]+\.\d{2}): (.+)$/
const RULE =
  /^Regla: lo que contenga «(.+)» va a (.+?)(, también en los movimientos que ya tienes)?$/
const TRANSFER = /^(Confirmar|Descartar) como transferencia: (.+) → (.+)$/
const CHECKPOINT =
  /^(.+): (saldo|adeudado) (-?[\d,]+\.\d{2}) al cierre del (\d{4}-\d{2}-\d{2})$/

interface Movement {
  date: string
  description: string
  cents: number
}

function centsOf(text: string): number {
  return Math.round(Number(text.replace(/,/g, '')) * 100)
}

function capitalize(text: string): string {
  return `${text.charAt(0).toUpperCase()}${text.slice(1)}`
}

function movement(text: string): Movement | null {
  const match = ENTRY.exec(text)
  if (!match) return null
  const [, date = '', description = '', amount = '0'] = match
  return { date, description, cents: centsOf(amount) }
}

function MovementMeta({ entry }: { entry: Movement }) {
  return (
    <>
      {formatCivilDayMonth(entry.date)} ·{' '}
      <Amount amount={entry.cents} size="inline" signed />
    </>
  )
}

export function describeProposal(proposal: Pick<Proposal, 'kind' | 'summary'>): {
  main: ReactNode
  meta?: ReactNode
} {
  const { summary } = proposal
  const strong = (text: string) => <strong className="font-semibold">{text}</strong>

  switch (proposal.kind) {
    case 'rule': {
      const match = RULE.exec(summary)
      if (!match) break
      const [, text = '', target = '', existing] = match
      return {
        main: (
          <>
            Si el concepto contiene {strong(text)} {`→\u00a0${capitalize(target)}`}
          </>
        ),
        meta: existing ? 'También en los movimientos que ya tienes' : undefined,
      }
    }

    case 'recategorize':
    case 'note': {
      const match = ENTRY_WITH_REST.exec(summary)
      if (!match) break
      const [, date = '', description = '', amount = '0', rest = ''] = match
      const entry = { date, description, cents: centsOf(amount) }
      const meta = <MovementMeta entry={entry} />

      if (proposal.kind === 'recategorize') {
        // "categoría Otros gastos", "transferencia entre tus cuentas" or both.
        const target = rest.replace(/^categoría /, '')
        return {
          main: (
            <>
              {strong(description)} {`→\u00a0${capitalize(target)}`}
            </>
          ),
          meta,
        }
      }

      const note = /^nota «(.*)»$/.exec(rest)
      return {
        main: note ? (
          <>
            {strong(description)} {`→\u00a0Nota «${note[1]}»`}
          </>
        ) : (
          <>Quitar la nota de {strong(description)}</>
        ),
        meta,
      }
    }

    case 'transfer_decision': {
      const match = TRANSFER.exec(summary)
      if (!match) break
      const [, verb, outText = '', inText = ''] = match
      const out = movement(outText)
      const into = movement(inText)
      if (!out || !into) break
      return {
        main:
          verb === 'Confirmar' ? (
            <>
              {strong(out.description)} y {strong(into.description)} son una transferencia
              entre tus cuentas
            </>
          ) : (
            <>
              {strong(out.description)} y {strong(into.description)} no son una
              transferencia
            </>
          ),
        meta: (
          <>
            {formatCivilDayMonth(out.date)} ·{' '}
            <Amount amount={out.cents} size="inline" signed /> →{' '}
            <Amount amount={into.cents} size="inline" signed />
          </>
        ),
      }
    }

    case 'checkpoint': {
      const match = CHECKPOINT.exec(summary)
      if (!match) break
      const [, name = '', kind, amount = '0', date = ''] = match
      return {
        main: (
          <>
            {kind === 'adeudado' ? 'Debías' : 'Tenías'}{' '}
            <Amount amount={centsOf(amount)} size="inline" className="font-semibold" /> en{' '}
            {strong(name)}
          </>
        ),
        meta: `Según el banco, al cierre del ${formatCivilDayMonth(date)}`,
      }
    }
  }

  return { main: summary }
}
