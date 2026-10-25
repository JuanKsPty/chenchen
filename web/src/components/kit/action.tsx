/**
 * KitAction: { label, to?, onClick?, icon?, disabled?, ariaLabel? }
 *
 * The one action a Notice or an EmptyState carries, described as data so the kit
 * decides how it looks. With `to` it renders a router <Link> styled as a Button
 * (onClick still runs); without it, a <button>. `icon` is a lucide icon, drawn at
 * 20px before the label. `ariaLabel` gives a fuller accessible name when several
 * actions on one screen share a label ("Revisar" → "Revisar transferencias"); it
 * must start with the visible label, so voice control still finds it.
 *
 * <ActionButton action variant? size? className? /> renders one. Kit-internal;
 * pages normally pass a KitAction to Notice or EmptyState instead.
 */
import type { LucideIcon } from 'lucide-react'
import type { ComponentProps } from 'react'
import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'

export interface KitAction {
  label: string
  /** A route to go to. */
  to?: string
  onClick?: () => void
  icon?: LucideIcon
  disabled?: boolean
  /** The accessible name, when the visible label alone is ambiguous. */
  ariaLabel?: string
}

export function ActionButton({
  action,
  variant,
  size,
  className,
}: {
  action: KitAction
  variant?: ComponentProps<typeof Button>['variant']
  size?: ComponentProps<typeof Button>['size']
  className?: string
}) {
  const Icon = action.icon
  const content = (
    <>
      {Icon && <Icon aria-hidden="true" />}
      {action.label}
    </>
  )

  if (action.to && !action.disabled) {
    return (
      <Button asChild variant={variant} size={size} className={className}>
        <Link to={action.to} onClick={action.onClick} aria-label={action.ariaLabel}>
          {content}
        </Link>
      </Button>
    )
  }

  return (
    <Button
      type="button"
      variant={variant}
      size={size}
      className={className}
      onClick={action.onClick}
      disabled={action.disabled}
      aria-label={action.ariaLabel}
    >
      {content}
    </Button>
  )
}
