/**
 * <FilterBar filters search onSearchChange onChange onClearAll accounts categories
 *            className? />
 *
 *   filters         what Movimientos is filtered by, read from the URL
 *   search          the search field's text as typed (the page debounces it)
 *   onSearchChange  every keystroke
 *   onChange        a change to one or more filters, merged into the URL by the page
 *   onClearAll      drops every filter and the search at once (one history entry)
 *   accounts        the owner's accounts, for "Cuenta"
 *   categories      the category tree, for "Categoría"
 *
 * Spec input/search: the search pill (560px at most from lg), and under it, 12px
 * apart, four filter chips 8px apart: Cuenta, Categoría (with "Sin categoría"
 * first), Fechas and Tipo. On a phone the chips scroll sideways with no scrollbar;
 * from lg they wrap. While anything is filtered a ghost "Quitar filtros" ends the
 * row; it clears everything in one tap and puts focus in the search field, since
 * the button itself goes away.
 */
import {
  ArrowDownLeftIcon,
  ArrowLeftRightIcon,
  ArrowUpRightIcon,
  CreditCardIcon,
  PercentIcon,
  ReceiptIcon,
  TrendingUpIcon,
  Undo2Icon,
  WalletIcon,
  type LucideIcon,
} from 'lucide-react'
import { useId, useMemo } from 'react'

import type { Account, CategoryNode, EntryKind } from '@/api/types'
import { SearchField } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { DESKTOP_QUERY, useMediaQuery } from '@/components/shell/media'
import { shortAccountLabel } from '@/lib/account'
import {
  UNCATEGORIZED_ICON,
  UNCATEGORIZED_LABEL,
  categoryIcon,
  categoryNames,
} from '@/lib/categories'
import { numberShort } from '@/lib/format'
import type { MovementFilters } from '@/lib/nav'
import { cn } from '@/lib/utils'
import { DateFilter } from './DateFilter'
import { OptionFilter, type FilterOption, type FilterOptionGroup } from './OptionFilter'

/** The "Tipo" choices, in the words of the chip once chosen. */
const KINDS: Array<{ kind: EntryKind; label: string; chip: string; icon: LucideIcon }> = [
  { kind: 'expense', label: 'Gastos', chip: 'Gastos', icon: ArrowUpRightIcon },
  { kind: 'income', label: 'Entradas', chip: 'Entradas', icon: ArrowDownLeftIcon },
  {
    kind: 'transfer',
    label: 'Transferencias entre tus cuentas',
    chip: 'Transferencias',
    icon: ArrowLeftRightIcon,
  },
  { kind: 'fee', label: 'Comisiones', chip: 'Comisiones', icon: ReceiptIcon },
  { kind: 'interest', label: 'Intereses', chip: 'Intereses', icon: PercentIcon },
  { kind: 'refund', label: 'Reembolsos', chip: 'Reembolsos', icon: Undo2Icon },
  {
    kind: 'trade',
    label: 'Compraventa de valores',
    chip: 'Compraventa de valores',
    icon: TrendingUpIcon,
  },
]

/** The value that stands for "Sin categoría" in the category picker. */
const UNCATEGORIZED = '__sin_categoria__'

function accountIcon(account: Account): LucideIcon {
  if (account.type === 'credit_card') return CreditCardIcon
  if (account.type === 'brokerage') return TrendingUpIcon
  return WalletIcon
}

/** The account's number at the right, unless its label already carries it. */
function accountDetail(account: Account): string | undefined {
  if (account.type === 'credit_card' || account.type === 'brokerage') return undefined
  return numberShort(account.external_number)
}

export function FilterBar({
  filters,
  search,
  onSearchChange,
  onChange,
  onClearAll,
  accounts,
  categories,
  className,
}: {
  filters: MovementFilters
  search: string
  onSearchChange: (value: string) => void
  onChange: (patch: Partial<MovementFilters>) => void
  onClearAll: () => void
  accounts: Account[]
  categories: CategoryNode[]
  className?: string
}) {
  const desktop = useMediaQuery(DESKTOP_QUERY)
  const names = useMemo(() => categoryNames(categories), [categories])
  const searchId = useId()
  const filtered =
    Boolean(search.trim()) ||
    Object.values(filters).some((value) => value !== undefined && value !== '')

  const account = filters.account
    ? accounts.find((candidate) => candidate.id === filters.account)
    : undefined
  const accountValue = filters.account
    ? account
      ? shortAccountLabel(account)
      : 'Una cuenta'
    : undefined

  const categoryValue = filters.uncategorized
    ? UNCATEGORIZED_LABEL
    : filters.category
      ? (names.get(filters.category) ?? 'Una categoría')
      : undefined

  const kindValue = filters.kind
    ? KINDS.find((option) => option.kind === filters.kind)?.chip
    : undefined

  const accountGroups: FilterOptionGroup[] = [
    {
      options: accounts.map((option) => ({
        value: option.id,
        label: shortAccountLabel(option),
        icon: accountIcon(option),
        detail: accountDetail(option),
      })),
    },
  ]

  const categoryGroups = useMemo(() => {
    const byKind = (kind: 'expense' | 'income'): FilterOption[] =>
      categories
        .filter((node) => node.kind === kind)
        .flatMap((node) => [
          { value: node.id, label: node.name, icon: categoryIcon(node.name) },
          ...node.children.map((child) => ({
            value: child.id,
            label: `${node.name} › ${child.name}`,
            icon: categoryIcon(`${node.name} › ${child.name}`),
          })),
        ])
    return [
      {
        options: [
          {
            value: UNCATEGORIZED,
            label: UNCATEGORIZED_LABEL,
            icon: UNCATEGORIZED_ICON,
            pending: true,
          },
        ],
      },
      { title: 'Gastos', options: byKind('expense') },
      { title: 'Entradas', options: byKind('income') },
    ] satisfies FilterOptionGroup[]
  }, [categories])

  const kindGroups: FilterOptionGroup[] = [
    {
      options: KINDS.map((option) => ({
        value: option.kind,
        label: option.label,
        icon: option.icon,
      })),
    },
  ]

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <SearchField
        id={searchId}
        value={search}
        onValueChange={onSearchChange}
        className="lg:max-w-140"
      />
      <div
        role="group"
        aria-label="Filtros"
        className={cn(
          // A phone scrolls the row sideways, edge to edge, with no scrollbar; the
          // 4px of vertical room keeps the focus outline from being clipped.
          '-mx-4 -my-1 flex gap-2 overflow-x-auto px-4 py-1 [scrollbar-width:none] md:-mx-8 md:px-8 [&::-webkit-scrollbar]:hidden',
          'lg:mx-0 lg:my-0 lg:flex-wrap lg:overflow-visible lg:px-0 lg:py-0',
        )}
      >
        <OptionFilter
          label="Cuenta"
          title="Filtrar por cuenta"
          value={accountValue}
          groups={accountGroups}
          desktop={desktop}
          onPick={(id) => onChange({ account: id })}
          onClear={() => onChange({ account: undefined })}
        />
        <OptionFilter
          label="Categoría"
          title="Filtrar por categoría"
          value={categoryValue}
          groups={categoryGroups}
          desktop={desktop}
          onPick={(id) =>
            onChange(
              id === UNCATEGORIZED
                ? { uncategorized: true, category: undefined }
                : { category: id, uncategorized: undefined },
            )
          }
          onClear={() => onChange({ category: undefined, uncategorized: undefined })}
        />
        <DateFilter
          from={filters.from}
          to={filters.to}
          desktop={desktop}
          onChange={(range) => onChange({ from: range.from, to: range.to })}
          onClear={() => onChange({ from: undefined, to: undefined })}
        />
        <OptionFilter
          label="Tipo"
          title="Filtrar por tipo"
          value={kindValue}
          groups={kindGroups}
          desktop={desktop}
          onPick={(kind) => onChange({ kind: kind as EntryKind })}
          onClear={() => onChange({ kind: undefined })}
        />
        {filtered && (
          <Button
            variant="ghost"
            className="shrink-0 px-4 lg:h-9"
            onClick={() => {
              onClearAll()
              document.getElementById(searchId)?.focus()
            }}
          >
            Quitar filtros
          </Button>
        )}
      </div>
    </div>
  )
}
