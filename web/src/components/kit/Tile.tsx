/**
 * <Tile institution size? className? />   an institution: its two or three initials
 *                                         ("IB") in Figtree 600, never a bank logo
 * <Tile icon pending? size? className? />  a category: a 20px lucide icon in muted;
 *                                         `pending` (Sin categoría) turns it
 *                                         primary-soft with the icon in primary-text
 *
 *   size  'sm' 40px (movement rows; the default for an icon)
 *         'md' 44px (account rows; the default for an institution)
 *         'lg' 56px (an account page's header)
 *
 * A surface2 circle. Always decorative (aria-hidden): the row beside it says what
 * it is in words.
 */
import type { LucideIcon } from 'lucide-react'

import type { Institution } from '@/api/types'
import { institutionInitials } from '@/lib/account'
import { cn } from '@/lib/utils'

const SIZES = {
  sm: 'size-10',
  md: 'size-11',
  lg: 'size-14',
} as const

type TileProps = {
  size?: keyof typeof SIZES
  className?: string
} & (
  | { institution: Institution | string; icon?: never; pending?: never }
  | { icon: LucideIcon; pending?: boolean; institution?: never }
)

export function Tile(props: TileProps) {
  const { className } = props

  if (props.institution !== undefined) {
    const size = props.size ?? 'md'
    return (
      <span
        aria-hidden="true"
        className={cn(
          'inline-flex shrink-0 items-center justify-center rounded-full bg-muted font-semibold text-foreground select-none',
          SIZES[size],
          size === 'lg' ? 'text-body' : 'text-small',
          className,
        )}
      >
        {institutionInitials(props.institution)}
      </span>
    )
  }

  const Icon = props.icon
  return (
    <span
      aria-hidden="true"
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-full',
        props.pending
          ? 'bg-primary-soft text-primary-text'
          : 'bg-muted text-muted-foreground',
        SIZES[props.size ?? 'sm'],
        className,
      )}
    >
      <Icon className="size-5" />
    </span>
  )
}
