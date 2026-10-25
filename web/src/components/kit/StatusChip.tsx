/**
 * <StatusChip status className? />
 *
 *   status  an AccountStatus from lib/account `accountStatus(account)` (not null)
 *
 * The spec's status chip: 28px pill, Figtree 14/20 600, 16px icon. Not interactive.
 *   cuadra        positive-soft, circle-check, "Cuadra"
 *   no-cuadra     solid attention fill, circle-alert, "No cuadra · $25.00". The only
 *                 orange fill in the app; never on the same row or card as a
 *                 primary button.
 *   sincronizado  surface2 and muted, refresh-cw, "Sincronizado hace 2 h"
 *   sin-partida   surface2 and muted, scale, "Sin saldo inicial". Neutral, never orange.
 */
import { CircleAlertIcon, CircleCheckIcon, RefreshCwIcon, ScaleIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import type { AccountStatus } from '@/lib/account'
import { relativeTime } from '@/lib/format'
import { Amount } from './Amount'

export function StatusChip({
  status,
  className,
}: {
  status: AccountStatus
  className?: string
}) {
  switch (status.kind) {
    case 'cuadra':
      return (
        <Badge variant="positive" className={className}>
          <CircleCheckIcon aria-hidden="true" />
          Cuadra
        </Badge>
      )
    case 'no-cuadra':
      return (
        <Badge variant="attention" className={className}>
          <CircleAlertIcon aria-hidden="true" />
          <span>
            No cuadra · <Amount amount={status.difference} size="inline" />
          </span>
        </Badge>
      )
    case 'sincronizado':
      return (
        <Badge variant="default" className={className}>
          <RefreshCwIcon aria-hidden="true" />
          Sincronizado {relativeTime(status.at)}
        </Badge>
      )
    case 'sin-partida':
      return (
        <Badge variant="default" className={className}>
          <ScaleIcon aria-hidden="true" />
          Sin saldo inicial
        </Badge>
      )
  }
}
