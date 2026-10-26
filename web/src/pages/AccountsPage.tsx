/**
 * Cuentas: every account the owner's files and the IBKR link brought in, grouped
 * the way a person thinks of them (what they have, what they owe, what they
 * invest), each group with its total.
 *
 * No artboard; built from spec accountRow (rows, group header and total) and
 * statusChip. Importing lives in the bars: the desktop bar's "Importar estado" and
 * the mobile bar's compact "Importar" (the route's default). "Conectar Interactive
 * Brokers" is the last row of Inversiones, which shows even with no investments.
 *
 * Layout: one column; where the page itself is at least 56rem wide (desktop
 * without the assistant pushing it), two columns balanced by row count, in
 * reading order. The empty state is the whole page and takes over the mobile bar's
 * Importar, so there is one primary in view.
 */
import { FileUpIcon, ScaleIcon } from 'lucide-react'

import type { AccountList } from '@/api/types'
import { ConnectIBKRRow } from '@/components/accounts/ConnectIBKRRow'
import { useImport } from '@/components/import/ImportProvider'
import { AccountGroup, EmptyState, Notice, PageHeader } from '@/components/kit'
import { LinkIBKRDialog } from '@/components/LinkIBKRDialog'
import { useTopBar } from '@/components/shell/TopBar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { amountOf, groupAccounts, type AccountGroup as Group } from '@/lib/account'
import { useAccounts } from '@/lib/queries'
import { useDocumentTitle } from '@/lib/title'

export function AccountsPage() {
  useDocumentTitle('Cuentas')
  const accounts = useAccounts()
  const { openImport } = useImport()
  const list = accounts.data?.accounts ?? []
  // A failed background refresh keeps what was already loaded on screen.
  const empty = accounts.data !== undefined && list.length === 0

  // The empty state's "Importar estado de cuenta" is the one primary; the bar's
  // compact Importar steps aside for it.
  useTopBar(empty ? { action: null } : {})

  return (
    <div className="flex flex-col gap-4 lg:gap-6">
      <PageHeader title="Cuentas" />
      {accounts.isPending ? (
        <AccountsSkeleton />
      ) : !accounts.data ? (
        <Notice
          title="No pudimos cargar tus cuentas"
          description="Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento."
          action={{ label: 'Reintentar', onClick: () => void accounts.refetch() }}
        />
      ) : empty ? (
        <EmptyState
          framed
          illustration="import"
          title="Importa tu primer estado de cuenta"
          description="Sube el archivo que te da tu banco y aquí te lo dejamos ordenadito."
          action={{
            label: 'Importar estado de cuenta',
            icon: FileUpIcon,
            onClick: () => openImport(),
          }}
        >
          <LinkIBKRDialog
            trigger={
              <Button variant="ghost" className="-mt-1 w-full md:w-auto">
                Conectar Interactive Brokers
              </Button>
            }
          />
        </EmptyState>
      ) : (
        <AccountGroups data={accounts.data} />
      )}
    </div>
  )
}

function AccountGroups({ data }: { data: AccountList }) {
  const groups = groupAccounts(data.accounts)
  const columns = balance(withInvestments(groups))
  const unanchored = data.accounts.filter((a) => !a.anchored && a.movements > 0).length

  return (
    <>
      {unanchored > 0 && (
        <Notice
          tone="pending"
          icon={ScaleIcon}
          title={
            unanchored === 1
              ? '1 cuenta sin saldo inicial'
              : `${unanchored} cuentas sin saldo inicial`
          }
          description={`${unanchored === 1 ? 'Ábrela' : 'Ábrelas'} y anota el saldo según tu banco para saber si cuadra.`}
        />
      )}
      <div className="@container">
        <div className="grid items-start gap-4 lg:gap-6 @4xl:grid-cols-2">
          {columns.map((column, index) => (
            <div key={index} className="flex min-w-0 flex-col gap-4 lg:gap-6">
              {column.map((group) => (
                <AccountGroup
                  key={group.key}
                  title={group.title}
                  total={group.accounts.length > 0 ? group.total : undefined}
                  accounts={group.accounts}
                >
                  {group.key === 'inversiones' && (
                    <ConnectIBKRRow another={group.accounts.length > 0} />
                  )}
                </AccountGroup>
              ))}
            </div>
          ))}
        </div>
      </div>
    </>
  )
}

/** Inversiones always shows, so connecting IBKR has a place to live. */
function withInvestments(groups: Group[]): Group[] {
  if (groups.some((g) => g.key === 'inversiones')) return groups
  const none: Group = {
    key: 'inversiones',
    title: 'Inversiones',
    accounts: [],
    total: amountOf(0),
  }
  return [...groups, none]
}

/**
 * Two columns for the wide layout, in reading order: each group goes to the
 * shorter column so far (a group weighs its rows plus its heading; Inversiones
 * also carries the connect row). Narrow, the columns simply stack.
 */
function balance(groups: Group[]): Group[][] {
  const columns: Group[][] = [[], []]
  const weight = [0, 0]
  for (const group of groups) {
    const side = weight[1]! < weight[0]! ? 1 : 0
    // Keep reading order: once something went right, the rest follows it.
    const target = columns[1]!.length > 0 ? 1 : side
    columns[target]!.push(group)
    weight[target]! += group.accounts.length + 1 + (group.key === 'inversiones' ? 1 : 0)
  }
  return columns.filter((column) => column.length > 0)
}

/** Shaped like the page it stands in for: two groups side by side when wide. */
function AccountsSkeleton() {
  return (
    <div role="status" aria-label="Cargando tus cuentas" className="@container">
      <div className="grid items-start gap-4 lg:gap-6 @4xl:grid-cols-2">
        {[2, 1].map((rows, index) => (
          <div
            key={index}
            className="flex flex-col gap-1 rounded-2xl bg-card p-1 shadow-card"
          >
            <div className="flex items-center justify-between px-4 pt-3 pb-1">
              <Skeleton className="h-5 w-20 rounded-full" />
              <Skeleton className="h-5 w-24 rounded-full" />
            </div>
            {Array.from({ length: rows }, (_, row) => (
              <div
                key={row}
                className="grid grid-cols-[2.75rem_minmax(0,1fr)_auto] items-center gap-3 px-4 py-3 md:min-h-18"
              >
                <Skeleton className="size-11 rounded-full" />
                <div className="flex flex-col gap-2">
                  <Skeleton className="h-4 w-32 max-w-full rounded-full" />
                  <Skeleton className="h-3.5 w-44 max-w-full rounded-full" />
                </div>
                <Skeleton className="h-4 w-20 rounded-full" />
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
