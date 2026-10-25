/**
 * <SettingsRow icon title description? meta? actions? className? />
 *
 *   icon         a lucide icon, drawn in the kit's 40px Tile (decorative)
 *   title        body 16/24 600, an <h3> under the card's <h2>
 *   description  small 14/20 500 muted, one or two short lines
 *   meta         under the description (a chip), with 4px above it
 *   actions      buttons at the right. When the row itself is narrower than
 *                28rem (a phone), they drop under the text, in the text's column.
 *
 * One line of an Ajustes card: "PIN", "Este teléfono". Rows are separated by space,
 * never by lines.
 */
import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { Tile } from '@/components/kit'
import { cn } from '@/lib/utils'

export function SettingsRow({
  icon,
  title,
  titleId,
  description,
  meta,
  actions,
  className,
}: {
  icon: LucideIcon
  title: ReactNode
  /** Gives the <h3> this id and makes it focusable from code (tabIndex -1), as a
   *  place to put focus when the row's own buttons go away. */
  titleId?: string
  description?: ReactNode
  meta?: ReactNode
  actions?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('@container', className)}>
      <div className="grid grid-cols-[2.5rem_minmax(0,1fr)] items-start gap-x-3 gap-y-3 @md:grid-cols-[2.5rem_minmax(0,1fr)_auto] @md:items-center">
        <Tile icon={icon} />
        <div className="flex min-w-0 flex-col">
          <h3
            id={titleId}
            tabIndex={titleId ? -1 : undefined}
            className="text-body font-semibold text-foreground"
          >
            {title}
          </h3>
          {description && (
            <p className="text-small font-medium text-pretty text-muted-foreground">
              {description}
            </p>
          )}
          {meta && <div className="mt-1 flex flex-wrap gap-2">{meta}</div>}
        </div>
        {actions && (
          <div className="col-start-2 flex flex-wrap items-center gap-2 @md:col-start-3 @md:justify-end">
            {actions}
          </div>
        )}
      </div>
    </div>
  )
}
