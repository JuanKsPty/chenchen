import { DownloadIcon, FileUpIcon } from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'

import { exportEntriesCSV, type EntryQuery } from '@/api/endpoints'
import type { Entry, EntryKind } from '@/api/types'
import { useImport } from '@/components/import/ImportProvider'
import { EmptyState, MovementList, Notice, PageHeader } from '@/components/kit'
import { FilterBar } from '@/components/movements/FilterBar'
import { useScrolled } from '@/components/shell/media'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import {
  movementFiltersToParams,
  movementFiltersToQuery,
  parseMovementFilters,
  type MovementFilters,
} from '@/lib/nav'
import { useAccounts, useCategories, useEntryPages } from '@/lib/queries'
import { useDocumentTitle } from '@/lib/title'
import { cn, downloadBlob } from '@/lib/utils'

/** Movements per "Ver más". */
const PAGE_SIZE = 40
/** Search waits this long after the last keystroke. */
const SEARCH_DELAY = 300

/**
 * "Sin categoría" means the spending to-dos, the kinds Gastos counts (the API's
 * SpendKinds): transfers between the owner's accounts and securities trades never
 * take a category, and income is Ingresos' own "Sin categoría" (Tipo: Entradas), so
 * they are left out unless a "Tipo" asks for them. That keeps the list equal to
 * Gastos' "Sin categoría · N movimientos".
 */
const NEEDS_CATEGORY: EntryKind[] = ['expense', 'refund', 'fee', 'interest']

const NO_ENTRIES: Entry[] = []

/**
 * Movimientos: every movement, searched and filtered, grouped by day.
 *
 * Every filter lives in the URL (lib/nav's frozen format), so a link from Inicio or
 * Gastos opens the same view, and Back undoes a filter. Search waits 300ms after
 * typing. Any change to the search or a filter starts the list again from the first
 * page; "Ver más" loads the next.
 *
 * Desktop (C-Movimientos-Desktop): the h1 with "Exportar CSV", the search, the chips,
 * and the list in a card with the 160px account column. Below lg
 * (C-Movimientos-Mobile): the search and the chips stay under the top bar while the
 * list scrolls, the day headings stick under them, and the rows run edge to edge.
 * Tapping a movement opens Categorizar.
 */
export function MovementsPage() {
  useDocumentTitle('Movimientos')
  const [params, setParams] = useSearchParams()
  const filters = useMemo(() => parseMovementFilters(params), [params])
  const accountsQuery = useAccounts()
  const categoriesQuery = useCategories()
  const { openImport } = useImport()
  const scrolled = useScrolled()

  const accounts = accountsQuery.data?.accounts ?? []
  const categories = categoriesQuery.data?.categories ?? []

  // --- filters ---------------------------------------------------------------

  // A chip change is a step Back can undo; the search replaces its own entry.
  const change = (patch: Partial<MovementFilters>) =>
    setParams((previous) =>
      movementFiltersToParams({ ...parseMovementFilters(previous), ...patch }),
    )

  // The search field holds what is typed; the URL gets it 300ms after the last key.
  const urlSearch = filters.search ?? ''
  const [text, setText] = useState(urlSearch)
  const [seenSearch, setSeenSearch] = useState(urlSearch)
  if (seenSearch !== urlSearch) {
    // Changed from outside (Back, a link, "Quitar filtros"): the field follows.
    setSeenSearch(urlSearch)
    if (text.trim() !== urlSearch) setText(urlSearch)
  }
  useEffect(() => {
    const next = text.trim()
    if (next === urlSearch) return
    const timer = window.setTimeout(
      () =>
        setParams(
          (previous) =>
            movementFiltersToParams({
              ...parseMovementFilters(previous),
              search: next || undefined,
            }),
          { replace: true },
        ),
      SEARCH_DELAY,
    )
    return () => window.clearTimeout(timer)
  }, [text, urlSearch, setParams])

  const clearAll = () => {
    setText('')
    setParams(new URLSearchParams())
  }

  const filtered =
    Boolean(filters.search) ||
    Boolean(filters.account) ||
    Boolean(filters.category) ||
    Boolean(filters.uncategorized) ||
    Boolean(filters.from) ||
    Boolean(filters.to) ||
    Boolean(filters.kind)

  // --- data ------------------------------------------------------------------

  const query = useMemo(() => {
    const base: Omit<EntryQuery, 'cursor'> = movementFiltersToQuery(filters)
    if (filters.uncategorized && !filters.kind) base.kinds = NEEDS_CATEGORY
    return base
  }, [filters])

  const pages = useEntryPages({ ...query, limit: PAGE_SIZE })
  const entries = useMemo(
    () => pages.data?.pages.flatMap((page) => page.entries) ?? NO_ENTRIES,
    [pages.data],
  )

  // While a new search or filter loads, the previous list stays, dimmed, rather
  // than flashing to skeletons on every keystroke.
  const [kept, setKept] = useState(entries)
  if (pages.data && kept !== entries) setKept(entries)
  const loadingNew = pages.isPending && kept.length > 0
  const shown = pages.data ? entries : kept

  // --- export ----------------------------------------------------------------

  // The buttons that wait (Exportar CSV, Ver más) say so with aria-disabled rather
  // than disabled: a button that turns disabled while it has the focus drops it to
  // the top of the page.
  const [exporting, setExporting] = useState(false)
  async function exportCSV() {
    if (exporting) return
    setExporting(true)
    try {
      // Every movement the filters match, not only the pages loaded.
      const { blob, filename } = await exportEntriesCSV(query)
      downloadBlob(blob, filename ?? 'movimientos.csv')
    } catch {
      toast('No se pudo exportar. Intenta otra vez en un momento.')
    } finally {
      setExporting(false)
    }
  }

  // --- "Ver más" ---------------------------------------------------------------

  const list = useRef<HTMLElement>(null)
  async function loadMore() {
    if (pages.isFetchingNextPage) return
    const before = entries.length
    const result = await pages.fetchNextPage()
    if (result.hasNextPage) return
    // The last page: "Ver más" goes away, so the focus moves on to the first
    // movement that just arrived instead of falling to the top of the page.
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        const active = document.activeElement
        if (active && active !== document.body) return
        list.current
          ?.querySelectorAll<HTMLElement>('button[aria-haspopup="dialog"]')
          [before]?.focus()
      }),
    )
  }

  // --- the sticky band's height, for the day headings under it ---------------

  // Measured from a ref callback, not a mount effect: the band is not there on a
  // first run (the empty state), and arrives later in the same page once the first
  // import brings the accounts.
  const [bandHeight, setBandHeight] = useState(0)
  const band = useCallback((element: HTMLDivElement | null) => {
    if (!element) return
    const observer = new ResizeObserver(() => setBandHeight(element.offsetHeight))
    observer.observe(element)
    setBandHeight(element.offsetHeight)
    return () => observer.disconnect()
  }, [])

  // --- states ----------------------------------------------------------------

  const noAccounts = accountsQuery.isSuccess && accounts.length === 0
  const nothingYet = !filtered && pages.isSuccess && entries.length === 0
  const firstRun = noAccounts || nothingYet

  const header = (
    <PageHeader
      title="Movimientos"
      className="max-lg:sr-only"
      actions={
        !firstRun && (
          <Button
            variant="secondary"
            className="hidden lg:inline-flex"
            onClick={() => void exportCSV()}
            aria-disabled={exporting || undefined}
          >
            {exporting ? <Spinner /> : <DownloadIcon aria-hidden="true" />}
            Exportar CSV
          </Button>
        )
      }
    />
  )

  if (firstRun) {
    return (
      <div className="flex flex-col gap-4 lg:gap-6">
        {header}
        <section aria-label="Movimientos">
          <EmptyState
            framed
            headingLevel={2}
            illustration="import"
            title="Importa tu primer estado de cuenta"
            description="Sube el archivo que te da tu banco y aquí te lo dejamos ordenadito."
            action={{
              label: 'Importar estado de cuenta',
              icon: FileUpIcon,
              onClick: () => openImport(),
            }}
          />
        </section>
      </div>
    )
  }

  const footer = shown.length > 0 && (
    <div className="flex flex-col items-center gap-1 px-4 pt-4 pb-2 lg:pt-2">
      {pages.hasNextPage && !loadingNew && (
        <Button
          variant="secondary"
          onClick={() => void loadMore()}
          aria-disabled={pages.isFetchingNextPage || undefined}
        >
          {pages.isFetchingNextPage && <Spinner />}
          Ver más
        </Button>
      )}
      <Button
        variant="ghost"
        className="lg:hidden"
        onClick={() => void exportCSV()}
        aria-disabled={exporting || undefined}
      >
        {exporting ? <Spinner /> : <DownloadIcon aria-hidden="true" />}
        Exportar CSV
      </Button>
    </div>
  )

  return (
    <div
      className="flex flex-col gap-4 lg:gap-6"
      style={{ '--movimientos-band': `${bandHeight}px` } as CSSProperties}
    >
      {header}

      <div
        ref={band}
        className={cn(
          // Below lg the search and the chips stay under the top bar, over its
          // shadow, and cast their own once the page scrolls.
          'sticky top-[calc(3.5rem+env(safe-area-inset-top))] z-31 -mx-4 -mt-2 bg-background px-4 pt-1 pb-2 transition-shadow duration-180 ease-out md:-mx-8 md:px-8',
          scrolled && 'shadow-bar',
          'lg:static lg:z-auto lg:mx-0 lg:mt-0 lg:bg-transparent lg:p-0 lg:shadow-none',
        )}
      >
        <FilterBar
          filters={filters}
          search={text}
          onSearchChange={setText}
          onChange={change}
          onClearAll={clearAll}
          accounts={accounts}
          categories={categories}
        />
      </div>

      {pages.isError ? (
        // The kit's notice drops "Reintentar" under the text on a phone, where the
        // side-by-side layout squeezed the message into a narrow column.
        <div role="alert">
          <Notice
            title="No pudimos cargar tus movimientos"
            description="Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento."
            action={{
              label: 'Reintentar',
              onClick: () => void pages.refetch(),
              disabled: pages.isFetching,
            }}
          />
        </div>
      ) : (
        <section
          ref={list}
          aria-label="Lista de movimientos"
          aria-busy={loadingNew || pages.isPending}
          className={cn(
            '-mx-4 -mt-5 lg:mx-0 lg:mt-0 lg:rounded-2xl lg:bg-card lg:p-2 lg:shadow-card',
            loadingNew && 'opacity-60 transition-opacity duration-180',
          )}
        >
          <MovementList
            entries={shown}
            accounts={accounts}
            categories={categories}
            loading={pages.isPending}
            headingLevel={2}
            dayHeadingClassName="max-lg:sticky max-lg:top-[calc(3.5rem+env(safe-area-inset-top)+var(--movimientos-band))] max-lg:z-10 max-lg:bg-background"
            footer={footer}
            empty={
              <EmptyState
                headingLevel={2}
                illustration={null}
                title="Nada con esos filtros"
                description="Prueba con otra búsqueda o quita los filtros."
                secondaryAction={{ label: 'Quitar filtros', onClick: clearAll }}
              />
            }
          />
        </section>
      )}
    </div>
  )
}
