/**
 * <RecentMovements accounts? className? />
 *
 *   accounts  the account list, to label each row's account ("Tarjeta ···1111")
 *
 * "Recientes": the five newest movements across every account, as one flat list
 * with the day in each row's second line (spec movementRow). Tapping a row opens
 * Categorizar. "Ver todos" goes to Movimientos.
 *
 * Always the compact row, the account in the second line ("Alimentación · 24 oct ·
 * Tarjeta ···1111"): the 160px account column is Movimientos' (spec movementRow),
 * and in Inicio's 7-column card it would squeeze the description to a few letters.
 * On a phone the line keeps the category and the day and drops the account, which
 * would only show as "Tarjet…" there.
 */
import { Link } from 'react-router-dom'

import type { Account } from '@/api/types'
import { MovementList, SectionCard } from '@/components/kit'
import { useMediaQuery } from '@/components/shell/media'
import { Button } from '@/components/ui/button'
import { MOVEMENTS_PATH } from '@/lib/nav'
import { useCategories, useEntries } from '@/lib/queries'

const SHOWN = 5

/** From md the row has room for the account after the category and the day. */
const ROOM_FOR_ACCOUNT = '(min-width: 768px)'

export function RecentMovements({
  accounts,
  className,
}: {
  accounts?: Account[]
  className?: string
}) {
  const recent = useEntries({ limit: SHOWN })
  const categories = useCategories()
  const showAccount = useMediaQuery(ROOM_FOR_ACCOUNT)

  return (
    <SectionCard
      title="Recientes"
      layout="list"
      action={
        <Button asChild variant="ghost">
          <Link to={MOVEMENTS_PATH} aria-label="Ver todos los movimientos">
            Ver todos
          </Link>
        </Button>
      }
      className={className}
    >
      <MovementList
        entries={(recent.data?.entries ?? []).slice(0, SHOWN)}
        accounts={accounts}
        categories={categories.data?.categories}
        showAccount={showAccount}
        groupByDay={false}
        loading={recent.isLoading}
        // Compact rows at every width (spec movementRow: only Movimientos gets the
        // 160px account column), so the account stays in each row's second line.
        compact
        empty={
          <p className="px-3 pt-1 pb-3 text-body text-pretty text-muted-foreground md:px-4">
            {recent.isError
              ? 'No pudimos cargar tus movimientos. Intenta otra vez en un momento.'
              : 'Aún no tienes movimientos.'}
          </p>
        }
      />
    </SectionCard>
  )
}
