/**
 * <RuleRow rule categoryName? selected? onEdit onDelete />
 *
 *   rule          the rule
 *   categoryName  its category's name (categoryNames(tree).get(rule.category_id))
 *   selected      the rule open in the form right now: keeps the row's hover fill
 *   onEdit        tapping the row: opens it in the form
 *   onDelete      the trash button: asks first (DeleteRuleDialog)
 *
 * An <li>. The row itself is a button, like a movement row: the category's tile,
 * the text the rule looks for in 600 (as the bank prints it, never restyled), and
 * what it does in small muted ("Va a Alimentación", "Cuenta como transferencia").
 * 64px tall on a phone, 56 from md; the 44px trash button sits outside it, in the
 * text colour (deleting is never orange).
 */
import { Trash2Icon } from 'lucide-react'

import type { CategoryRule } from '@/api/types'
import { Tile } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { categoryIcon, TRANSFER_ICON } from '@/lib/categories'
import { cn } from '@/lib/utils'

/** "Va a Alimentación", "Cuenta como transferencia", or both. */
export function ruleOutcome(rule: CategoryRule, categoryName?: string): string {
  const parts: string[] = []
  if (rule.category_id) parts.push(`Va a ${categoryName ?? 'una categoría'}`)
  if (rule.transfer) {
    parts.push(parts.length ? 'cuenta como transferencia' : 'Cuenta como transferencia')
  }
  return parts.join(' y ')
}

export function RuleRow({
  rule,
  categoryName,
  selected = false,
  onEdit,
  onDelete,
}: {
  rule: CategoryRule
  categoryName?: string
  selected?: boolean
  onEdit: (rule: CategoryRule) => void
  onDelete: (rule: CategoryRule) => void
}) {
  const icon = rule.category_id ? categoryIcon(categoryName) : TRANSFER_ICON

  return (
    <li data-rule-id={rule.id} className="flex items-center gap-1 md:pr-2">
      <button
        type="button"
        onClick={() => onEdit(rule)}
        className={cn(
          'grid min-h-16 min-w-0 flex-1 cursor-pointer grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-3 rounded-full px-3 py-2.5 text-left md:px-4 text-foreground transition-colors duration-180 ease-out hover:bg-muted active:bg-muted md:min-h-14 md:py-2',
          selected && 'bg-muted',
        )}
      >
        <Tile icon={icon} />
        <span className="flex min-w-0 flex-col">
          <span className="sr-only">Editar la regla </span>
          <span className="truncate text-body font-semibold">{rule.match_text}</span>
          <span className="truncate text-small font-medium text-muted-foreground">
            {ruleOutcome(rule, categoryName)}
          </span>
        </span>
      </button>
      <Button
        variant="plain"
        size="icon"
        aria-label={`Eliminar la regla ${rule.match_text}`}
        onClick={() => onDelete(rule)}
      >
        <Trash2Icon aria-hidden="true" />
      </Button>
    </li>
  )
}
