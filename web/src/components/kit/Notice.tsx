/**
 * <Notice tone? title description? action? icon? className? />
 *
 *   tone         'attention' (default)  "No cuadra" and real errors: attention-soft
 *                                       ground, circle-alert in attention, and the
 *                                       action as the "Revisar" button (surface,
 *                                       attention text).
 *                'pending'              something waiting, not wrong ("2 cuentas sin
 *                                       saldo inicial", "3 transferencias por
 *                                       confirmar"): a surface card, icon in
 *                                       primary-text, the action as a secondary button.
 *   title        ReactNode, body 16/24 600. Put amounts in <Amount size="inline">.
 *   description  ReactNode, small 14/20 500 muted ("Desde el 03 oct.").
 *   action       KitAction { label, to?, onClick?, icon? }, a 44px button.
 *   icon         a lucide icon to replace the tone's default (24px).
 *
 * Layout per spec card/surface "Aviso No cuadra": radius 16, padding 16, grid
 * [24px | 1fr | auto], gap 12, centred. When the notice itself is narrower than
 * 24rem (mobile) the action drops under the text, in the second column.
 */
import { CircleAlertIcon, CircleHelpIcon, type LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'
import { ActionButton, type KitAction } from './action'

const TONES = {
  attention: {
    box: 'bg-attention-soft',
    icon: 'text-attention',
    defaultIcon: CircleAlertIcon,
    button: 'notice',
  },
  pending: {
    box: 'bg-card shadow-card',
    icon: 'text-primary-text',
    defaultIcon: CircleHelpIcon,
    button: 'secondary',
  },
} as const

export function Notice({
  tone = 'attention',
  title,
  description,
  action,
  icon,
  className,
}: {
  tone?: keyof typeof TONES
  title: ReactNode
  description?: ReactNode
  action?: KitAction
  icon?: LucideIcon
  className?: string
}) {
  const style = TONES[tone]
  const Icon = icon ?? style.defaultIcon

  return (
    <div className={cn('@container rounded-2xl p-4', style.box, className)}>
      <div className="grid grid-cols-[1.5rem_minmax(0,1fr)] items-start gap-x-3 gap-y-3 @sm:grid-cols-[1.5rem_minmax(0,1fr)_auto] @sm:items-center">
        <Icon aria-hidden="true" className={cn('size-6 shrink-0', style.icon)} />
        <div className="flex min-w-0 flex-col">
          <p className="text-body font-semibold text-pretty text-foreground">{title}</p>
          {description && (
            <p className="text-small font-medium text-pretty text-muted-foreground">
              {description}
            </p>
          )}
        </div>
        {action && (
          <div className="col-start-2 @sm:col-start-3">
            <ActionButton action={action} variant={style.button} />
          </div>
        )}
      </div>
    </div>
  )
}
