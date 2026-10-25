/**
 * <SectionCard title? action? layout? headingLevel? as? id? className? children />
 *
 *   title         the card's heading, Gabarito 700 22/28. Labels the <section>.
 *   action        ReactNode at the right of the heading, normally a ghost button:
 *                 <Button variant="ghost" asChild><Link to=…>Ver todos</Link></Button>
 *   layout        'default'  padding 16 on mobile and 24 from md (spec card/surface).
 *                 'list'     padding 4 / 8 so rows (AccountRow, MovementRow,
 *                            day headings) reach the edge with their own padding;
 *                            the heading is inset to line up with the rows' content.
 *   headingLevel  2 (default) or 3.
 *   as            'section' (default) or 'div'. A section is labelled by its title.
 *
 * Surface, radius 16, card shadow, no border. Header: a 44px row, title and action
 * centred, 12px above the content; the heading at the same offset in both layouts
 * (24 / 32 from the top, 16 / 24 from the left), and a ghost action's label flush
 * with the content's right edge.
 */
import { useId, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

export function SectionCard({
  title,
  action,
  layout = 'default',
  headingLevel = 2,
  as: Tag = 'section',
  id,
  className,
  children,
}: {
  title?: ReactNode
  action?: ReactNode
  layout?: 'default' | 'list'
  headingLevel?: 2 | 3
  as?: 'section' | 'div'
  id?: string
  className?: string
  children?: ReactNode
}) {
  const headingId = useId()
  const Heading = headingLevel === 3 ? 'h3' : 'h2'
  const list = layout === 'list'
  const hasHeader = Boolean(title || action)

  return (
    <Tag
      id={id}
      aria-labelledby={title ? headingId : undefined}
      className={cn(
        'flex min-w-0 flex-col rounded-2xl bg-card text-card-foreground shadow-card',
        list ? 'p-1 md:p-2' : 'p-4 md:p-6',
        className,
      )}
    >
      {hasHeader && (
        <div
          className={cn(
            // A 44px row in both layouts, with or without an action, so the
            // heading sits at the same place in every card: the card's padding
            // plus 8 from the top (24 / 32), the padding from the left (16 / 24).
            'box-content flex min-h-11 items-center justify-between gap-3',
            list ? 'pt-3 pr-0 pb-1 pl-3 md:pt-4 md:pr-1 md:pb-2 md:pl-4' : 'mb-3',
          )}
        >
          {title ? (
            <Heading id={headingId} className="font-display text-heading font-bold">
              {title}
            </Heading>
          ) : (
            <span />
          )}
          {action && (
            // A ghost action's label lines up with the content's right edge (the
            // amounts under it): its 12px of padding hangs out into the card's.
            <div
              className={cn(
                'flex shrink-0 items-center gap-1',
                !list && '*:data-[variant=ghost]:-mr-3',
              )}
            >
              {action}
            </div>
          )}
        </div>
      )}
      {children}
    </Tag>
  )
}
