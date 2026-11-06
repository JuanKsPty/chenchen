import {
  ArrowLeftRightIcon,
  BriefcaseIcon,
  CarIcon,
  CircleDashedIcon,
  CircleHelpIcon,
  GraduationCapIcon,
  HeartPulseIcon,
  HouseIcon,
  RepeatIcon,
  ShoppingBagIcon,
  ShoppingBasketIcon,
  TicketIcon,
  TrendingUpIcon,
  ZapIcon,
  type LucideIcon,
} from 'lucide-react'

import type { Category, CategoryNode, Entry } from '@/api/types'

/**
 * Categories as the app shows them.
 *
 *   categoryNames(tree)       Map id → name; a child reads "Padre › Hija"
 *   categoryOptions(tree, kind)
 *                             the categories of one kind ('expense' | 'income') as
 *                             [{ id, name }] for a <select>, children as "Padre › Hija",
 *                             sorted by name
 *   categoryIcon(name)        the lucide icon for a category name (spec iconStyle),
 *                             circle-dashed when the name is not one we know
 *   UNCATEGORIZED_ICON        circle-help: "Sin categoría", a to-do, not an error
 *   TRANSFER_ICON             arrow-left-right: a transfer between the owner's accounts
 *   UNCATEGORIZED_LABEL       "Sin categoría"
 *   movementCategory(entry, categoryName?)
 *                             { label, icon, pending } for a movement's tile and meta
 *                             line: its category (pass categoryNames(tree).get(id)),
 *                             "Transferencia", the trade label, or "Sin categoría"
 *                             (pending)
 */

export const UNCATEGORIZED_LABEL = 'Sin categoría'
export const UNCATEGORIZED_ICON: LucideIcon = CircleHelpIcon
export const TRANSFER_ICON: LucideIcon = ArrowLeftRightIcon

/** Every category's name by id, children as "Padre › Hija". */
export function categoryNames(tree: CategoryNode[] | undefined): Map<string, string> {
  const names = new Map<string, string>()
  for (const node of tree ?? []) {
    names.set(node.id, node.name)
    for (const child of node.children) names.set(child.id, `${node.name} › ${child.name}`)
  }
  return names
}

/** A category as one choice in a picker. */
export interface CategoryOption {
  id: string
  name: string
}

/**
 * The categories of one kind as picker choices, sorted by name. A child takes its
 * parent's kind and reads "Padre › Hija".
 */
export function categoryOptions(
  tree: CategoryNode[] | undefined,
  kind: Category['kind'],
): CategoryOption[] {
  const names = categoryNames(tree)
  const options: CategoryOption[] = []
  for (const node of tree ?? []) {
    if (node.kind !== kind) continue
    options.push({ id: node.id, name: names.get(node.id) ?? node.name })
    for (const child of node.children) {
      options.push({ id: child.id, name: names.get(child.id) ?? child.name })
    }
  }
  return options.sort((a, b) => a.name.localeCompare(b.name, 'es'))
}

/** Lower case, no accents, so "Alimentacion" and "ALIMENTACIÓN" find the same icon. */
function normalize(name: string): string {
  return name.normalize('NFD').replace(/[̀-ͯ]/g, '').trim().toLowerCase()
}

const ICONS: Record<string, LucideIcon> = {
  alimentacion: ShoppingBasketIcon,
  transporte: CarIcon,
  vivienda: HouseIcon,
  servicios: ZapIcon,
  salud: HeartPulseIcon,
  entretenimiento: TicketIcon,
  compras: ShoppingBagIcon,
  educacion: GraduationCapIcon,
  suscripciones: RepeatIcon,
  transferencia: ArrowLeftRightIcon,
  salario: BriefcaseIcon,
  freelance: BriefcaseIcon,
  inversiones: TrendingUpIcon,
  'otros gastos': CircleDashedIcon,
  'otros ingresos': CircleDashedIcon,
}

/**
 * The icon for a category, by name. A child category ("Padre › Hija") uses its own
 * name first and falls back to its parent's. Unknown names get circle-dashed.
 */
export function categoryIcon(name: string | undefined | null): LucideIcon {
  if (!name) return CircleDashedIcon
  const parts = name.split('›').map(normalize)
  for (const part of parts.reverse()) {
    const icon = ICONS[part]
    if (icon) return icon
  }
  return CircleDashedIcon
}

/** How a movement is labelled: its category, or why it has none. */
export function movementCategory(
  entry: Pick<Entry, 'kind'>,
  categoryName: string | undefined,
): { label: string; icon: LucideIcon; pending: boolean } {
  if (categoryName) {
    return { label: categoryName, icon: categoryIcon(categoryName), pending: false }
  }
  if (entry.kind === 'transfer')
    return { label: 'Transferencia', icon: TRANSFER_ICON, pending: false }
  if (entry.kind === 'trade') {
    return { label: 'Compraventa de valores', icon: TrendingUpIcon, pending: false }
  }
  return { label: UNCATEGORIZED_LABEL, icon: UNCATEGORIZED_ICON, pending: true }
}
