/**
 * Reglas (/gastos/reglas): "movements whose concept contains X go to category Y",
 * or "count as a transfer between my accounts". A rule never overwrites a category
 * chosen by hand.
 *
 * Desktop (lg and up): the rules on the left (7 of 12 columns) and the form on the
 * right (5), sticky under the bar; tapping a rule opens it in that form. When the
 * page is narrower than 56rem (the assistant panel open), they stack.
 * Below lg: the rules alone, with "Crear" in the mobile bar (and a chevron back to
 * Gastos); the form opens in a bottom sheet, for a new rule or a tapped one.
 *
 * Deleting always asks first (DeleteRuleDialog). Focus after a delete goes to the
 * row that took its place (the next one, or the one before when it was the last);
 * after saving from the sheet it goes back to what opened it, or to the saved rule
 * when that was the empty state's button.
 */
import { PlusIcon } from 'lucide-react'
import { useCallback, useMemo, useRef, useState } from 'react'

import type { CategoryRule } from '@/api/types'
import { EmptyState, PageHeader, SectionCard } from '@/components/kit'
import { DeleteRuleDialog } from '@/components/settings/DeleteRuleDialog'
import { problemMessage } from '@/components/settings/problem'
import { RuleForm } from '@/components/settings/RuleForm'
import { focusWhenReady } from '@/components/settings/focus'
import { RuleRow } from '@/components/settings/RuleRow'
import { DESKTOP_QUERY, useMediaQuery } from '@/components/shell/media'
import { useTopBar } from '@/components/shell/TopBar'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { categoryNames } from '@/lib/categories'
import { useReturnFocus } from '@/lib/focus'
import { measure } from '@/lib/layout'
import { SPENDING_PATH } from '@/lib/nav'
import { useCategories, useRules } from '@/lib/queries'
import { useDocumentTitle } from '@/lib/title'
import { cn } from '@/lib/utils'

const INTRO =
  'Ordenan tus movimientos cada vez que importas. Nunca cambian lo que categorizaste a mano.'

export function RulesPage() {
  useDocumentTitle('Reglas')
  const desktop = useMediaQuery(DESKTOP_QUERY)
  const rules = useRules()
  const categories = useCategories()
  const tree = categories.data?.categories
  const names = useMemo(() => categoryNames(tree), [tree])
  const returnFocus = useReturnFocus()
  const listRef = useRef<HTMLDivElement>(null)
  const formRef = useRef<HTMLDivElement>(null)

  /** The rule open in the form; null is a new one. */
  const [editing, setEditing] = useState<CategoryRule | null>(null)
  /** Remounts the form with fresh fields whenever what it edits changes. */
  const [formKey, setFormKey] = useState(0)
  const [focusForm, setFocusForm] = useState(false)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [deleting, setDeleting] = useState<CategoryRule | null>(null)
  /** The row that takes focus once the rule being deleted is gone. */
  const neighbor = useRef<string | null>(null)
  /** The rule the sheet just saved, for focus when what opened the sheet is gone. */
  const savedId = useRef<string | null>(null)

  const list = rules.data?.rules ?? []
  const empty = rules.isSuccess && list.length === 0

  const rowButton = (id: string | null) =>
    id
      ? listRef.current?.querySelector<HTMLElement>(`li[data-rule-id="${id}"] > button`)
      : null

  function askDelete(rule: CategoryRule) {
    const index = list.findIndex((item) => item.id === rule.id)
    neighbor.current = (list[index + 1] ?? list[index - 1])?.id ?? null
    setDeleting(rule)
  }

  const open = useCallback(
    (rule: CategoryRule | null) => {
      setEditing(rule)
      setFormKey((key) => key + 1)
      setFocusForm(true)
      if (!desktop) setSheetOpen(true)
    },
    [desktop],
  )

  const startNew = useCallback(() => open(null), [open])

  const barAction = useMemo(
    () =>
      empty ? null : (
        // A verb, like every button (spec voice); the full name is for screen readers.
        <Button
          data-rule-new
          className="pr-4 pl-3"
          aria-label="Crear regla"
          onClick={startNew}
        >
          <PlusIcon aria-hidden="true" />
          Crear
        </Button>
      ),
    [empty, startNew],
  )
  useTopBar({ title: 'Reglas', back: SPENDING_PATH, action: barAction })

  function saved(rule: CategoryRule) {
    savedId.current = rule.id
    if (desktop) {
      // Back to a fresh "Nueva regla", with the text field focused for the next one.
      setEditing(null)
      setFormKey((key) => key + 1)
      setFocusForm(true)
    } else {
      setSheetOpen(false)
    }
  }

  return (
    <div className="flex flex-col gap-4 lg:gap-6">
      <PageHeader title="Reglas" subtitle={INTRO} />
      <p className={cn('text-body text-pretty text-muted-foreground lg:hidden', measure)}>
        {INTRO}
      </p>

      <div className="@container">
        <div className="grid gap-4 lg:gap-6 @4xl:grid-cols-12 @4xl:items-start">
          <SectionCard title="Tus reglas" className="@4xl:col-span-7">
            <div ref={listRef} className="contents">
              {rules.isPending ? (
                <div className="flex flex-col gap-1" aria-hidden="true">
                  {[0, 1, 2].map((row) => (
                    <div key={row} className="flex min-h-16 items-center gap-3 md:min-h-14">
                      <Skeleton className="size-10 shrink-0 rounded-full" />
                      <div className="flex flex-1 flex-col gap-2">
                        <Skeleton className="h-4 w-36" />
                        <Skeleton className="h-3.5 w-28" />
                      </div>
                    </div>
                  ))}
                </div>
              ) : rules.isError ? (
                <EmptyState
                  illustration={null}
                  headingLevel={3}
                  title="No pudimos cargar tus reglas"
                  description={problemMessage(rules.error)}
                  // Ghost: "Crear regla" (form or bar) stays the view's one primary.
                  secondaryAction={{
                    label: 'Reintentar',
                    onClick: () => void rules.refetch(),
                  }}
                />
              ) : empty ? (
                <EmptyState
                  headingLevel={3}
                  title="Todavía no tienes reglas"
                  description={
                    desktop
                      ? 'Crea la primera aquí al lado. Tus próximos estados de cuenta se ordenan solos.'
                      : 'Crea una y tus próximos estados de cuenta se ordenan solos.'
                  }
                  action={
                    desktop
                      ? undefined
                      : { label: 'Crear regla', icon: PlusIcon, onClick: startNew }
                  }
                />
              ) : (
                <ul className="-mx-3 -mb-1 flex flex-col gap-1 md:-mx-4 md:-mb-2">
                  {list.map((rule) => (
                    <RuleRow
                      key={rule.id}
                      rule={rule}
                      categoryName={
                        rule.category_id ? names.get(rule.category_id) : undefined
                      }
                      selected={desktop && editing?.id === rule.id}
                      onEdit={open}
                      onDelete={askDelete}
                    />
                  ))}
                </ul>
              )}
            </div>
          </SectionCard>

          {desktop && (
            <SectionCard
              title={editing ? 'Editar regla' : 'Nueva regla'}
              className="@4xl:sticky @4xl:top-20 @4xl:col-span-5"
            >
              <div ref={formRef} className="contents">
                <RuleForm
                  key={formKey}
                  rule={editing}
                  categories={tree}
                  layout="card"
                  autoFocus={focusForm}
                  onSaved={saved}
                  onCancel={editing ? startNew : undefined}
                />
              </div>
            </SectionCard>
          )}
        </div>
      </div>

      {!desktop && (
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent
            side="bottom"
            aria-describedby={undefined}
            onOpenAutoFocus={() => {
              savedId.current = null
              returnFocus.onOpenAutoFocus()
            }}
            onCloseAutoFocus={(event) => {
              returnFocus.onCloseAutoFocus(event)
              if (event.defaultPrevented) return
              // What opened the sheet is gone (the empty state's "Crear regla" after
              // the first rule): the saved rule's row, else the bar's "Crear".
              const id = savedId.current
              savedId.current = null
              event.preventDefault()
              focusWhenReady(
                () =>
                  rowButton(id) ??
                  listRef.current?.querySelector<HTMLElement>('li > button') ??
                  document.querySelector<HTMLElement>('[data-rule-new]'),
              )
            }}
          >
            <SheetHeader>
              <SheetTitle>{editing ? 'Editar regla' : 'Nueva regla'}</SheetTitle>
            </SheetHeader>
            <RuleForm
              key={formKey}
              rule={editing}
              categories={tree}
              layout="sheet"
              onSaved={saved}
            />
          </SheetContent>
        </Sheet>
      )}

      <DeleteRuleDialog
        rule={deleting}
        onOpenChange={(next) => {
          if (!next) setDeleting(null)
        }}
        onDeleted={(rule) => {
          if (editing?.id !== rule.id) return
          setEditing(null)
          setFormKey((key) => key + 1)
          setFocusForm(false)
        }}
        fallbackFocus={(gone) => {
          // The row that took its place (next, or previous when it was the last),
          // else the empty state's button (mobile), else the form's text field
          // (desktop).
          const next = neighbor.current
          neighbor.current = null
          return (
            rowButton(next) ??
            listRef.current?.querySelector<HTMLElement>(
              `li:not([data-rule-id="${gone.id}"]) > button`,
            ) ??
            listRef.current?.querySelector<HTMLElement>(':scope > div button') ??
            formRef.current?.querySelector('input')
          )
        }}
      />
    </div>
  )
}
