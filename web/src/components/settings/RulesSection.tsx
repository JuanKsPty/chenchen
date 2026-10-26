/**
 * <RulesSection className? />
 *
 * Ajustes › Reglas: how many rules there are, what they do, and "Ver reglas", the
 * way into /gastos/reglas from Ajustes (Gastos has the other one).
 */
import { Link } from 'react-router-dom'

import { SectionCard } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { RULES_PATH } from '@/lib/nav'
import { useRules } from '@/lib/queries'

export function RulesSection({ className }: { className?: string }) {
  const rules = useRules()
  const count = rules.data?.rules.length ?? 0

  return (
    <SectionCard
      title="Reglas"
      className={className}
      action={
        // Like every "Ver …" in a card header (Ver todos, Ver reglas in Gastos): a
        // ghost label, no chevron.
        <Button asChild variant="ghost">
          <Link to={RULES_PATH}>Ver reglas</Link>
        </Button>
      }
    >
      {rules.isPending ? (
        <Skeleton className="h-5 w-64 max-w-full" aria-hidden="true" />
      ) : rules.isError ? (
        // No count to give: never claim there are none when they did not load.
        <p className="text-body text-pretty text-muted-foreground">
          Tus reglas ordenan tus movimientos cada vez que importas.
        </p>
      ) : (
        <p className="text-body text-pretty text-muted-foreground">
          {count === 0
            ? 'Crea reglas y tus movimientos se ordenan solos cada vez que importas.'
            : `${count === 1 ? '1 regla ordena' : `${count} reglas ordenan`} tus movimientos cada vez que importas.`}
        </p>
      )}
    </SectionCard>
  )
}
