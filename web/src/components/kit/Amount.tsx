/**
 * <Amount amount size? signed? tone? className? />
 *
 *   amount  Amount | number (integer cents)
 *   size    'list' (default)  Figtree 16/24 600, cents the same size so a column of
 *                             them aligns. Rows, totals, the Patrimonio breakdown.
 *           'inline'          inherits size, weight and colour: inside a sentence
 *                             ("No cuadra por $25.00", "Mínimo $25.00").
 *           'heading'         Gabarito 700 22/28 (the card's "Debes $486.20").
 *           'title'           Gabarito 800 32/36, cents at 22 (account balance).
 *           'display'         Gabarito 800 48/52, cents at 32 (net worth).
 *   signed  false (default)   a balance: no "+", a negative still reads "−$12.00".
 *           true              a movement: "+$3,800.00" in positive green,
 *                             "−$84.37" in the text colour. Never red or orange.
 *   tone    'auto' (default)  positive for a signed inflow, otherwise the parent's
 *                             colour (so it is white on the Patrimonio card).
 *           'muted'           muted-foreground, e.g. a balance with no starting point.
 *           'positive'        forces green; only for money coming in.
 *
 * Always tabular figures. The sign is U+2212 or "+", glued to the "$".
 */
import type { Amount as AmountValue } from '@/api/types'
import { moneyParts } from '@/lib/format'
import { cn } from '@/lib/utils'

export type AmountSize = 'inline' | 'list' | 'heading' | 'title' | 'display'
export type AmountTone = 'auto' | 'muted' | 'positive'

const SIZES: Record<AmountSize, string> = {
  inline: '',
  list: 'text-body font-semibold',
  heading: 'font-display text-heading font-bold',
  title: 'font-display text-title font-extrabold',
  display: 'font-display text-display font-extrabold',
}

/** Smaller cents, same weight, colour and baseline: only on the big figures. */
const CENTS: Partial<Record<AmountSize, string>> = {
  title: 'text-heading',
  display: 'text-title',
}

export function Amount({
  amount,
  size = 'list',
  signed = false,
  tone = 'auto',
  className,
}: {
  amount: AmountValue | number
  size?: AmountSize
  signed?: boolean
  tone?: AmountTone
  className?: string
}) {
  const cents = typeof amount === 'number' ? amount : amount.cents
  const { whole, cents: fraction } = moneyParts(cents, { signed })
  const inflow = signed && cents > 0

  const color =
    tone === 'muted'
      ? 'text-muted-foreground'
      : tone === 'positive' || inflow
        ? 'text-positive'
        : undefined

  const smallCents = CENTS[size]

  return (
    <span className={cn('whitespace-nowrap tabular-nums', SIZES[size], color, className)}>
      {smallCents ? (
        <>
          {whole}
          <span className={smallCents}>{fraction}</span>
        </>
      ) : (
        `${whole}${fraction}`
      )}
    </span>
  )
}
