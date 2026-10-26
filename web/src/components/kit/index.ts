/**
 * The chenchén kit: the app's own components, built on ui/* and the tokens.
 * Each file starts with its props. Import from here:
 *
 *   import { Amount, MovementList, SectionCard } from '@/components/kit'
 *
 *   Amount         money, always: list, inline, heading, title and display sizes
 *   StatusChip     Cuadra / No cuadra · $25.00 / Sincronizado hace 2 h / Sin saldo inicial
 *   Notice         the No cuadra notice ("Revisar"), or a pending one
 *   SectionCard    the surface card with a heading and an optional ghost action
 *   PageHeader     a page's h1 (screen-reader only on mobile) and its actions
 *   EmptyState     illustration, title, text and the primary action
 *   Tile           the 40/44/56 circle: institution initials or a category icon
 *   AccountRow     one account, linking to its page (AccountRowSubtitle: its second line)
 *   AccountGroup   Cuentas / Tarjetas / Inversiones with their total
 *   MovementRow    one movement
 *   MovementList   movements grouped by day; tapping one opens Categorizar
 *   CategoryBar    a category's total and bar, or the "Sin categoría" to-do
 *   FilterChip     a filter's pill: off with a chevron, on with its value and an x
 *   SearchField    the search pill with a hidden label and a clear button
 *   KitAction      { label, to?, onClick?, icon?, disabled?, ariaLabel? } for Notice and
 *                  EmptyState
 */
export { AccountGroup } from './AccountGroup'
export { AccountRow, AccountRowSubtitle } from './AccountRow'
export { Amount, type AmountSize, type AmountTone } from './Amount'
export { CategoryBar } from './CategoryBar'
export { EmptyState } from './EmptyState'
export { FilterChip } from './FilterChip'
export { MovementList } from './MovementList'
export { MovementRow } from './MovementRow'
export { Notice } from './Notice'
export { PageHeader } from './PageHeader'
export { SearchField } from './SearchField'
export { SectionCard } from './SectionCard'
export { StatusChip } from './StatusChip'
export { Tile } from './Tile'
export type { KitAction } from './action'
