/**
 * <PageFallback />
 *
 * What <main> shows while a page's chunk is still on its way (pages/lazy): the bars
 * stay, and the page area holds the shape of the page that is coming, drawn like the
 * pages' own loading states (surface cards, radius 16, the card shadow, a heading and
 * rows of placeholders), so the page slides into place instead of the layout jumping.
 *
 * Every page starts the same way: from lg the h1's 36px line, then the content on the
 * pages' rhythm (16px apart below lg, 24 from lg). What follows depends on the section:
 *   Inicio       the veranera Patrimonio block and Recientes (7) beside the lists (5)
 *   Cuentas      the account groups, two side by side when there is room
 *   a cuenta     the balance, then movements (7) beside the bank's figures (5)
 *   Movimientos  the search, the filter chips and the list (a card from lg)
 *   Gastos       the month (beside the title from lg), the outflow card, categories
 *   Reglas       the intro, the rules (7) beside the form (5)
 *   Ajustes      the settings cards, 7 + 5 when there is room
 *
 * Rarely seen: the first page arrives with the shell, and the shell fetches the rest
 * as soon as the browser is idle. Nothing here imports a page, so the shell's chunk
 * stays small.
 */
import type { ReactNode } from 'react'
import { matchPath, useLocation } from 'react-router-dom'

import { Skeleton } from '@/components/ui/skeleton'
import {
  ACCOUNTS_PATH,
  HOME_PATH,
  MOVEMENTS_PATH,
  RULES_PATH,
  SETTINGS_PATH,
  SPENDING_PATH,
} from '@/lib/nav'
import { cn } from '@/lib/utils'

export function PageFallback() {
  const { pathname } = useLocation()

  return (
    <div aria-busy="true">
      <span role="status" className="sr-only">
        Cargando…
      </span>
      <div aria-hidden="true">{shapeFor(pathname)}</div>
    </div>
  )
}

function shapeFor(pathname: string): ReactNode {
  if (pathname === HOME_PATH) return <HomeShape />
  if (pathname === ACCOUNTS_PATH) return <AccountsShape />
  if (matchPath(`${ACCOUNTS_PATH}/:id`, pathname)) return <AccountShape />
  if (pathname === MOVEMENTS_PATH) return <MovementsShape />
  if (pathname === SPENDING_PATH) return <SpendingShape />
  if (pathname === RULES_PATH) return <SplitShape intro rows={[6, 4]} />
  if (pathname === SETTINGS_PATH) return <SplitShape rows={[2, 2]} />
  return (
    <Page>
      <Card rows={5} />
    </Page>
  )
}

/** The pages' outer column: the h1's line from lg, then 16 / 24 apart. */
function Page({ action, children }: { action?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-4 lg:gap-6">
      <div className="flex items-start justify-between gap-4 max-lg:hidden">
        <Skeleton className="h-9 w-64 max-w-full rounded-full" />
        {action}
      </div>
      {children}
    </div>
  )
}

function HomeShape() {
  return (
    <Page>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-12 lg:gap-6">
        <div className="flex min-w-0 flex-col gap-4 lg:col-span-7 lg:gap-6">
          {/* Patrimonio: the app's one solid block, at least 280px from lg. */}
          <div className="flex flex-col justify-between gap-4 rounded-2xl bg-primary p-4 md:p-6 lg:min-h-70 lg:gap-8">
            <div className="flex flex-col gap-2">
              <Skeleton className="h-5 w-32 rounded-full bg-primary-foreground/20" />
              <Skeleton className="h-13 w-56 max-w-full rounded-full bg-primary-foreground/20" />
            </div>
            <div className="grid grid-cols-3 gap-4 lg:gap-6">
              {[0, 1, 2].map((column) => (
                <Skeleton
                  key={column}
                  className="h-10 w-full max-w-24 rounded-2xl bg-primary-foreground/20"
                />
              ))}
            </div>
          </div>
          <Card rows={5} className="max-lg:hidden" />
        </div>
        <div className="flex min-w-0 flex-col gap-4 lg:col-span-5 lg:gap-6">
          <Card rows={4} />
        </div>
      </div>
    </Page>
  )
}

function AccountsShape() {
  return (
    <Page>
      <div className="@container">
        <div className="grid items-start gap-4 lg:gap-6 @4xl:grid-cols-2">
          <Card rows={2} />
          <Card rows={1} />
        </div>
      </div>
    </Page>
  )
}

function AccountShape() {
  return (
    // As AccountPage: 16 under the mobile bar, 40 between blocks from lg.
    <div className="flex flex-col gap-6 max-lg:pt-4 lg:gap-10">
      <div className="flex flex-col gap-6">
        <div className="flex items-center gap-4 max-lg:hidden">
          <Skeleton className="size-14 rounded-full" />
          <div className="flex flex-col gap-2">
            <Skeleton className="h-8 w-56 rounded-full" />
            <Skeleton className="h-4 w-40 rounded-full" />
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Skeleton className="h-4 w-24 rounded-full" />
          <Skeleton className="h-9 w-48 rounded-full" />
        </div>
      </div>
      <div className="grid gap-4 lg:grid-cols-12 lg:items-start lg:gap-6">
        <div className="flex min-w-0 flex-col gap-3 lg:col-span-7 lg:gap-0 lg:rounded-2xl lg:bg-card lg:p-2 lg:shadow-card">
          <div className="box-content flex min-h-11 items-center pl-1 lg:pt-4 lg:pb-2 lg:pl-4">
            <Skeleton className="h-6 w-36 rounded-full" />
          </div>
          <div className="rounded-2xl bg-card p-1 shadow-card lg:bg-transparent lg:p-0 lg:shadow-none">
            <Rows count={5} />
          </div>
        </div>
        <Card rows={4} className="max-lg:hidden lg:col-span-5" />
      </div>
    </div>
  )
}

function MovementsShape() {
  return (
    <Page action={<Skeleton className="h-11 w-36 rounded-full" />}>
      <div className="flex flex-col gap-3">
        <Skeleton className="h-11 w-full rounded-full lg:max-w-140" />
        <div className="flex gap-2 overflow-hidden">
          {['w-24', 'w-28', 'w-24', 'w-20'].map((width, chip) => (
            <Skeleton
              key={chip}
              className={cn('h-11 shrink-0 rounded-full lg:h-9', width)}
            />
          ))}
        </div>
      </div>
      {/* Rows on the page below lg, as the list draws them; a card from lg. */}
      <div className="-mx-4 lg:mx-0 lg:rounded-2xl lg:bg-card lg:p-2 lg:shadow-card">
        <div className="flex min-h-9 items-end px-4 pt-3 pb-1">
          <Skeleton className="h-3.5 w-24 rounded-full" />
        </div>
        <Rows count={6} />
      </div>
    </Page>
  )
}

function SpendingShape() {
  return (
    <Page action={<Skeleton className="h-9 w-40 rounded-full" />}>
      <Skeleton className="h-11 w-full rounded-full md:max-w-sm lg:hidden" />
      {/* OutflowCard's own layout: the figure, and the chart beside it when wide. */}
      <div className="flex flex-wrap gap-6 rounded-2xl bg-card p-4 shadow-card md:gap-8 md:p-6">
        <div className="flex min-w-0 flex-[1_1_15rem] flex-col gap-2">
          <Skeleton className="h-5 w-36 rounded-full" />
          <Skeleton className="h-13 w-56 max-w-full rounded-full" />
        </div>
        <Skeleton className="h-60 min-w-0 flex-[1.4_1_18.75rem] rounded-2xl" />
      </div>
      <Card rows={3} />
    </Page>
  )
}

/** Reglas and Ajustes: one column, then 7 + 5 once the column is 56rem wide. */
function SplitShape({ intro = false, rows }: { intro?: boolean; rows: [number, number] }) {
  return (
    <Page>
      {intro && (
        <div className="flex flex-col gap-2 lg:-mt-4">
          <Skeleton className="h-4 w-full max-w-md rounded-full" />
          <Skeleton className="h-4 w-48 rounded-full lg:hidden" />
        </div>
      )}
      <div className="@container">
        <div className="grid gap-4 lg:gap-6 @4xl:grid-cols-12 @4xl:items-start">
          <Card rows={rows[0]} className="@4xl:col-span-7" />
          <Card rows={rows[1]} className="@4xl:col-span-5" />
        </div>
      </div>
    </Page>
  )
}

/** A list card (SectionCard layout="list"): the 44px header row, then the rows. */
function Card({ rows, className }: { rows: number; className?: string }) {
  return (
    <div
      className={cn(
        'flex min-w-0 flex-col rounded-2xl bg-card p-1 shadow-card md:p-2',
        className,
      )}
    >
      <div className="box-content flex min-h-11 items-center pt-3 pb-1 pl-3 md:pt-4 md:pb-2 md:pl-4">
        <Skeleton className="h-6 w-36 rounded-full" />
      </div>
      <Rows count={rows} />
    </div>
  )
}

/** MovementRow's shape: tile, two lines, an amount. */
function Rows({ count }: { count: number }) {
  return (
    <div className="flex flex-col">
      {Array.from({ length: count }, (_, row) => (
        <div
          key={row}
          className="grid min-h-16 grid-cols-[2.5rem_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 md:min-h-14 md:py-2"
        >
          <Skeleton className="size-10 rounded-full" />
          <div className="flex flex-col gap-2">
            <Skeleton className="h-4 w-40 max-w-full rounded-full" />
            <Skeleton className="h-3 w-28 max-w-full rounded-full" />
          </div>
          <Skeleton className="h-4 w-16 rounded-full" />
        </div>
      ))}
    </div>
  )
}
