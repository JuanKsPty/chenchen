/**
 * <AccountMovements account className? />
 *
 * The account's movements, grouped by day, newest first, 20 at a time with "Ver más"
 * (useEntryPages). "Ver todos" opens Movimientos filtered to this account. Tapping a
 * row opens Categorizar (MovementList does that itself).
 *
 * Mobile (spec C-Cuenta-Mobile): the "Movimientos" heading sits above the card. From
 * lg (C-Cuenta-Desktop) the heading moves inside one card with the rows.
 */
import { useId } from 'react'
import { FileUpIcon } from 'lucide-react'
import { Link } from 'react-router-dom'

import type { Account } from '@/api/types'
import { useImport } from '@/components/import/ImportProvider'
import { EmptyState, MovementList } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { movementsHref } from '@/lib/nav'
import { useCategories, useEntryPages } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { LOAD_FAILED } from './bank'

const PAGE = 20

export function AccountMovements({
  account,
  className,
}: {
  account: Account
  className?: string
}) {
  const headingId = useId()
  const { openImport } = useImport()
  const pages = useEntryPages({ accountId: account.id, limit: PAGE })
  const categories = useCategories()
  const entries = pages.data?.pages.flatMap((page) => page.entries) ?? []
  const brokerage = account.type === 'brokerage'

  return (
    <section
      aria-labelledby={headingId}
      className={cn(
        'flex min-w-0 flex-col gap-3 lg:gap-0 lg:rounded-2xl lg:bg-card lg:p-2 lg:shadow-card',
        className,
      )}
    >
      {/* From lg the card header of a list card (SectionCard layout="list"): a 44px
          row with or without "Ver todos", 16px down and in from the card's 8, level
          with the side card's header. */}
      <div className="box-content flex min-h-11 items-center justify-between gap-3 pl-1 lg:pt-4 lg:pr-1 lg:pb-2 lg:pl-4">
        <h2 id={headingId} className="font-display text-heading font-bold">
          Movimientos
        </h2>
        {account.movements > 0 && (
          <Button variant="ghost" asChild>
            <Link to={movementsHref({ account: account.id })}>Ver todos</Link>
          </Button>
        )}
      </div>
      <div className="rounded-2xl bg-card p-1 shadow-card lg:bg-transparent lg:p-0 lg:shadow-none">
        <MovementList
          entries={entries}
          accounts={[account]}
          categories={categories.data?.categories}
          showAccount={false}
          loading={pages.isLoading}
          empty={
            pages.isError ? (
              <p className="px-3 py-6 text-center text-body text-pretty text-muted-foreground">
                {LOAD_FAILED}
              </p>
            ) : brokerage ? (
              <EmptyState
                illustration={null}
                headingLevel={3}
                title="Todavía no hay movimientos"
                description="Sincroniza con Interactive Brokers para traerlos."
              />
            ) : (
              <EmptyState
                illustration="import"
                headingLevel={3}
                title="Esta cuenta no tiene movimientos"
                description="Sube el archivo que te da tu banco y aquí te lo dejamos ordenadito."
                action={{
                  label: 'Importar estado de cuenta',
                  icon: FileUpIcon,
                  onClick: () => openImport({ accountId: account.id }),
                }}
              />
            )
          }
          footer={
            pages.hasNextPage && (
              <div className="flex justify-center px-2 pt-1 pb-2">
                {/* Secondary, as in Movimientos. aria-disabled, not disabled, while
                    it loads: a button that turns disabled drops the focus. */}
                <Button
                  variant="secondary"
                  onClick={() => {
                    if (!pages.isFetchingNextPage) void pages.fetchNextPage()
                  }}
                  aria-disabled={pages.isFetchingNextPage || undefined}
                >
                  {pages.isFetchingNextPage && <Spinner />}
                  Ver más
                </Button>
              </div>
            )
          }
        />
      </div>
    </section>
  )
}
