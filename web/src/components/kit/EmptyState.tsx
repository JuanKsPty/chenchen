/**
 * <EmptyState title description? illustration? action? secondaryAction?
 *             headingLevel? className? children? />
 *
 *   title            Gabarito 700 22/28 ("Importa tu primer estado de cuenta").
 *   description      body 16/24 muted.
 *   illustration     'empty' (default) | 'import' | null. The brand <Illustration>:
 *                    200×150 from md, 160×120 on mobile, decorative (alt=""). In dark
 *                    mode it sits on a light tile rather than being hidden.
 *   action           KitAction: the primary button, 48px and full width on mobile,
 *                    44px from md. E.g. { label: 'Importar estado de cuenta',
 *                    icon: FileUpIcon, onClick: () => openImport() }.
 *   secondaryAction  KitAction drawn as a ghost button under it
 *                    ("Conectar Interactive Brokers", "Quitar filtros").
 *   headingLevel     2 (default), 3, or 1 when the empty state is the whole page (404).
 *   framed           false (default). true puts it in a surface card (radius 16, card
 *                    shadow): a page's first-run state before any import (Inicio,
 *                    Cuentas, Movimientos, Gastos), drawn the same on all four.
 *   children         anything else, under the actions.
 *
 * Spec emptyState: centred column, padding 32 24, gap 12, at most 360px wide. Works
 * inside a card or as a whole page. No frame, no dashed border.
 */
import type { ReactNode } from 'react'

import { Illustration } from '@/components/brand'
import { cn } from '@/lib/utils'
import { ActionButton, type KitAction } from './action'

export function EmptyState({
  title,
  description,
  illustration = 'empty',
  action,
  secondaryAction,
  headingLevel = 2,
  framed = false,
  className,
  children,
}: {
  title: ReactNode
  description?: ReactNode
  illustration?: 'empty' | 'import' | null
  action?: KitAction
  secondaryAction?: KitAction
  headingLevel?: 1 | 2 | 3
  framed?: boolean
  className?: string
  children?: ReactNode
}) {
  const Heading = headingLevel === 1 ? 'h1' : headingLevel === 3 ? 'h3' : 'h2'

  const state = (
    <div
      className={cn(
        'mx-auto flex w-full max-w-90 flex-col items-center gap-3 px-6 py-8 text-center',
        className,
      )}
    >
      {illustration && <Illustration name={illustration} className="mb-1" />}
      <Heading className="font-display text-heading font-bold text-balance">
        {title}
      </Heading>
      {description && (
        <p className="text-body text-pretty text-muted-foreground">{description}</p>
      )}
      {(action || secondaryAction) && (
        <div className="mt-2 flex w-full flex-col items-stretch gap-2 md:w-auto md:items-center">
          {action && <ActionButton action={action} size="lg" className="md:h-11" />}
          {secondaryAction && <ActionButton action={secondaryAction} variant="ghost" />}
        </div>
      )}
      {children}
    </div>
  )

  if (!framed) return state
  return (
    <div className="flex justify-center rounded-2xl bg-card px-2 py-4 text-card-foreground shadow-card md:py-10">
      {state}
    </div>
  )
}
