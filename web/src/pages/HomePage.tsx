import { HomeEmpty } from '@/components/home/HomeEmpty'
import { HomeNotices } from '@/components/home/HomeNotices'
import { MonthFlow } from '@/components/home/MonthFlow'
import { NetWorthCard } from '@/components/home/NetWorthCard'
import { RecentMovements } from '@/components/home/RecentMovements'
import { TopCategories } from '@/components/home/TopCategories'
import { Notice, PageHeader } from '@/components/kit'
import { useAccounts } from '@/lib/queries'
import { useSession } from '@/lib/session'
import { useDocumentTitle } from '@/lib/title'

/**
 * Inicio: how the owner is doing, at a glance (C-Inicio-Desktop, C-Inicio-Mobile,
 * spec pageLayout).
 *
 * From lg, a 12-column grid with 24px gaps: Patrimonio (7) beside the notices and
 * "En qué se te va" (5); under them Recientes (7) and "Este mes" (5), each column
 * flowing on its own height. Below lg, one column 16px apart, in reading order:
 * Patrimonio, notices, En qué se te va, Este mes, Recientes. That is at most five
 * figures before the first list.
 *
 * The greeting is the page's h1 from lg ("Buenas, Juan. Así va tu chenchén.");
 * below lg the mobile bar shows it, and the h1 stays for screen readers.
 * Before the first import, the empty state: Importar, or connect IBKR.
 */
export function HomePage() {
  useDocumentTitle('Inicio')
  const { user } = useSession()
  const accounts = useAccounts()
  const firstName = user?.full_name.trim().split(/\s+/)[0]

  const list = accounts.data?.accounts
  // From the data, not the status: a failed background refresh keeps what was
  // already loaded (an empty list included), as Cuentas does.
  const empty = list?.length === 0

  let body
  if (accounts.isError && !accounts.data) {
    body = (
      <Notice
        title="No pudimos cargar tu resumen"
        description="Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento."
        action={{ label: 'Reintentar', onClick: () => void accounts.refetch() }}
      />
    )
  } else if (empty) {
    body = <HomeEmpty />
  } else {
    // From lg the two columns flow on their own: the right one (notices, En qué se
    // te va, Este mes) spans both rows, and the second row is `1fr` so that it, not
    // the first, takes up the right column's extra height. Patrimonio then keeps its
    // own height instead of stretching into a tall veranera slab, and Recientes
    // starts right under it. DOM order is the phone's reading order.
    body = (
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-12 lg:grid-rows-[auto_1fr] lg:gap-6">
        <NetWorthCard
          accounts={list}
          netWorth={accounts.data?.net_worth}
          className="lg:col-span-7 lg:col-start-1 lg:row-start-1"
        />
        <div className="flex min-w-0 flex-col gap-4 lg:col-span-5 lg:col-start-8 lg:row-span-2 lg:row-start-1 lg:gap-6 lg:self-start">
          {list && <HomeNotices accounts={list} />}
          <TopCategories />
          <MonthFlow />
        </div>
        <RecentMovements
          accounts={list}
          className="lg:col-span-7 lg:col-start-1 lg:row-start-2 lg:self-start"
        />
      </div>
    )
  }

  return (
    // 24 under the h1 from lg, as on every other page.
    <div className="flex flex-col gap-4 lg:gap-6">
      <PageHeader
        title={
          <>
            {firstName ? `Buenas, ${firstName}.` : 'Buenas.'}{' '}
            <span className="text-muted-foreground">Así va tu chenchén.</span>
          </>
        }
      />
      {body}
    </div>
  )
}
