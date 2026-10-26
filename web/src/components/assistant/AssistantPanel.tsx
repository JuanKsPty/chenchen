/**
 * <AssistantPanel variant="panel"|"sheet" onClose? className? />
 *
 * The assistant's header, transcript and composer. A pure renderer: the
 * conversation lives in AssistantProvider, so the shell can mount this in the
 * desktop panel or the mobile sheet without either owning a second copy.
 *
 *   panel  (spec assistantPanel) inside the shell's 400px floating card. Header
 *          56: the 20px assistant icon in primary-text and "Asistente" in heading
 *          22; 40px icon buttons. Body padding 20, gap 16. Composer padding
 *          12 16 16, a 48px pill with a 40px send button 4px from its edge.
 *   sheet  (spec assistantSheet) inside the shell's full-screen sheet, which
 *          already pads both safe areas. Header 56 with 44px icon buttons and 24px
 *          icons; body padding 16; chips, buttons and send are 44px. Composer
 *          padding 8 16.
 *
 * What the transcript shows, top to bottom:
 *   - the intro (voice.assistantIntro), always;
 *   - with no conversation yet, the three suggestion chips;
 *   - each question as a primary-soft bubble on the right, each reply as plain
 *     text with its figures in 600 and, above it, a muted line naming what the
 *     assistant looked at (the tool trace);
 *   - a reply's proposals right under it (the stream's `proposal` events), and
 *     the ones no turn on screen produced (after a reload) after the stored
 *     history, so whatever is asked in this visit stays below them;
 *   - on the sheet, once a conversation exists, the suggestions not yet asked, in
 *     a row that scrolls sideways (C-Asistente-Mobile).
 * When the engine is not the model (no key, budget spent, model down), one muted
 * line under the header says so.
 */
import { Fragment, useId, useRef, useState } from 'react'
import {
  ArrowUpIcon,
  CircleAlertIcon,
  InfoIcon,
  Loader2Icon,
  Trash2Icon,
  XIcon,
} from 'lucide-react'

import type { ChatProvider, Proposal } from '@/api/types'
import { AssistantIcon } from '@/components/brand'
import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Bubble, BubbleContent } from '@/components/ui/bubble'
import { Button } from '@/components/ui/button'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Message, MessageContent } from '@/components/ui/message'
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '@/components/ui/message-scroller'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Skeleton } from '@/components/ui/skeleton'
import { useProposals } from '@/lib/queries'
import { cn } from '@/lib/utils'
import {
  STORED_PREFIX,
  TOOL_LABELS,
  useAssistant,
  useAssistantProposals,
  type AnnouncedProposal,
  type ToolRun,
  type Turn,
} from './AssistantProvider'
import { ProposalList } from './ProposalCard'
import { renderReply } from './replyText'

type Variant = 'panel' | 'sheet'

const INTRO =
  'Pregúntame lo que quieras de tu chenchén. Yo reviso tus movimientos; tú decides qué aplicar.'

const SUGGESTIONS = [
  '¿Cuánto gasté en comida este mes?',
  '¿Qué cuenta no cuadra?',
  'Ordena lo que está sin categoría',
]

export function AssistantPanel({
  className,
  variant = 'panel',
  onClose,
}: {
  className?: string
  /** 'panel' is the desktop aside, 'sheet' the full-screen mobile sheet. */
  variant?: Variant
  /** Closes the panel or the sheet. */
  onClose?: () => void
}) {
  const sheet = variant === 'sheet'
  const {
    turns,
    draft,
    setDraft,
    streaming,
    activeTool,
    failure,
    dismissFailure,
    send,
    clear,
    clearing,
    provider,
    loadingHistory,
  } = useAssistant()
  const { byTurn, applied, dismissed, orderOf } = useAssistantProposals()
  const proposals = useProposals()
  const rootRef = useRef<HTMLDivElement>(null)
  const inputId = useId()
  // Discarding a card removes the button that had focus; the panel takes it.
  const refocus = () => rootRef.current?.focus({ preventScroll: true })

  const groups = groupTurns(turns)
  const placed = placeProposals(groups, byTurn, {
    pending: proposals.data?.proposals ?? [],
    fetchedAt: proposals.dataUpdatedAt,
    applied,
    dismissed,
    orderOf,
  })
  // Proposals no reply on screen made (after a reload, or left over from a
  // cleared conversation) close the stored history: they are older than
  // anything asked in this visit, so a new question and its answer go below
  // them and stay the last thing in the transcript.
  const lastStored = lastStoredIndex(groups)
  const orphans = (
    <ProposalList proposals={placed.unplaced} size={variant} onGone={refocus} />
  )

  const asked = new Set(
    turns.filter((turn) => turn.role === 'user').map((turn) => turn.text),
  )
  const remaining = SUGGESTIONS.filter((suggestion) => !asked.has(suggestion))
  const note = provider ? engineNote(provider) : null
  const canSend = !streaming && draft.trim().length > 0

  return (
    <div
      ref={rootRef}
      tabIndex={-1}
      className={cn('flex min-h-0 flex-col bg-card outline-none', className)}
    >
      <header
        className={cn(
          'flex h-14 shrink-0 items-center gap-2',
          sheet ? 'pr-1 pl-4' : 'pr-2 pl-5',
        )}
      >
        <AssistantIcon
          variant={sheet ? 'mobile' : 'desktop'}
          className={cn('text-primary-text', sheet ? 'size-6' : 'size-5')}
        />
        {sheet ? (
          // The sheet's own (screen-reader) title already names the dialog.
          <p
            aria-hidden="true"
            className="min-w-0 flex-1 font-display text-heading font-bold"
          >
            Asistente
          </p>
        ) : (
          <h2 className="min-w-0 flex-1 font-display text-heading font-bold">Asistente</h2>
        )}
        {turns.length > 0 && (
          <ClearConversation
            variant={variant}
            disabled={clearing || streaming}
            onConfirm={clear}
          />
        )}
        {onClose && (
          <Button
            variant="plain"
            size={sheet ? 'icon' : 'icon-bar'}
            onClick={onClose}
            aria-label="Cerrar asistente"
          >
            <XIcon aria-hidden="true" className={sheet ? 'size-6' : undefined} />
          </Button>
        )}
      </header>

      {note && (
        <p
          className={cn(
            'flex shrink-0 items-start gap-2 pb-2 text-small text-pretty text-muted-foreground',
            sheet ? 'px-4' : 'px-5',
          )}
        >
          <InfoIcon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
          {note}
        </p>
      )}

      <MessageScrollerProvider autoScroll defaultScrollPosition="end">
        <MessageScroller className="flex-1">
          <MessageScrollerViewport
            aria-label="Conversación"
            className={sheet ? 'p-4' : 'p-5'}
          >
            <MessageScrollerContent className="gap-4">
              <p className="text-body text-pretty text-muted-foreground">{INTRO}</p>

              {loadingHistory ? (
                <>
                  <ChatSkeleton />
                  {orphans}
                </>
              ) : (
                <>
                  {turns.length === 0 && (
                    <SuggestionChips
                      variant={variant}
                      items={SUGGESTIONS}
                      disabled={streaming}
                      onPick={(text) => send(text)}
                    />
                  )}

                  {lastStored < 0 && orphans}

                  {groups.map((group, index) => {
                    const linked = placed.byGroup.get(group.id) ?? []
                    // A reply still waiting for its first event has nothing to show
                    // yet; an empty item would add a second gap above "Pensando…".
                    const hidden =
                      group.kind === 'reply' && linked.length === 0 && isEmpty(group)
                    return (
                      <Fragment key={group.id}>
                        {!hidden && (
                          <MessageScrollerItem
                            messageId={group.id}
                            className="flex flex-col gap-4"
                          >
                            {group.kind === 'user' ? (
                              <Question text={group.text} />
                            ) : (
                              <>
                                <Reply group={group} />
                                <ProposalList
                                  proposals={linked}
                                  size={variant}
                                  onGone={refocus}
                                />
                              </>
                            )}
                          </MessageScrollerItem>
                        )}
                        {index === lastStored && orphans}
                      </Fragment>
                    )
                  })}
                </>
              )}

              {streaming && <Working tool={activeTool} />}

              {failure && (
                <Alert variant="attention">
                  <CircleAlertIcon aria-hidden="true" />
                  <AlertTitle>El asistente se interrumpió</AlertTitle>
                  <AlertDescription>{failure}</AlertDescription>
                  <AlertAction>
                    <Button
                      variant="plain"
                      size={sheet ? 'icon' : 'icon-bar'}
                      onClick={dismissFailure}
                      aria-label="Cerrar aviso"
                    >
                      <XIcon aria-hidden="true" />
                    </Button>
                  </AlertAction>
                </Alert>
              )}

              {sheet &&
                !loadingHistory &&
                turns.length > 0 &&
                !streaming &&
                remaining.length > 0 && (
                  <SuggestionChips
                    variant={variant}
                    items={remaining}
                    scroll
                    onPick={(text) => send(text)}
                  />
                )}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton size={sheet ? 'icon' : 'icon-sm'} />
        </MessageScroller>
      </MessageScrollerProvider>

      <form
        onSubmit={(event) => {
          event.preventDefault()
          send()
        }}
        className={cn('shrink-0 bg-card', sheet ? 'px-4 py-2' : 'px-4 pt-3 pb-4')}
      >
        <label htmlFor={inputId} className="sr-only">
          Pregunta al asistente
        </label>
        <InputGroup className="md:h-12">
          <InputGroupInput
            id={inputId}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="Pregunta sobre tu chenchén…"
            maxLength={2000}
            autoComplete="off"
            enterKeyHint="send"
          />
          <InputGroupAddon
            align="inline-end"
            className={sheet ? 'has-[>button]:pr-0.5' : undefined}
          >
            {/* aria-disabled, not disabled: sending empties the draft, and a
                disabled button would drop the focus to <body>, out of the panel
                and out of reach of its Escape. send() ignores an empty draft. */}
            <InputGroupButton
              type="submit"
              variant="default"
              size={sheet ? 'icon' : 'icon-sm'}
              aria-disabled={!canSend || undefined}
              aria-label="Enviar"
              className="aria-disabled:translate-y-0 aria-disabled:cursor-not-allowed aria-disabled:bg-muted aria-disabled:text-muted-foreground aria-disabled:hover:bg-muted"
            >
              {streaming ? (
                <Loader2Icon aria-hidden="true" className="animate-spin" />
              ) : (
                <ArrowUpIcon aria-hidden="true" />
              )}
            </InputGroupButton>
          </InputGroupAddon>
        </InputGroup>
      </form>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

/** "Borrar conversación": the conversation cannot come back, so it asks first. */
function ClearConversation({
  variant,
  disabled,
  onConfirm,
}: {
  variant: Variant
  disabled: boolean
  onConfirm: () => void
}) {
  const [open, setOpen] = useState(false)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const titleId = useId()
  const descriptionId = useId()
  const sheet = variant === 'sheet'
  const size = sheet ? 'default' : 'bar'

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="plain"
          size={sheet ? 'icon' : 'icon-bar'}
          disabled={disabled}
          aria-label="Borrar conversación"
          className="disabled:bg-transparent"
        >
          <Trash2Icon aria-hidden="true" className={sheet ? 'size-6' : undefined} />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        role="alertdialog"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
        align="end"
        sideOffset={8}
        // Focus starts on the safe choice: two quick Enters must not delete.
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          cancelRef.current?.focus()
        }}
        className="w-76 max-w-[calc(100vw-2rem)]"
      >
        <div className="flex flex-col gap-1">
          <p id={titleId} className="font-semibold">
            ¿Borrar la conversación?
          </p>
          <p id={descriptionId} className="text-small text-muted-foreground">
            Se borran las preguntas y las respuestas. Las propuestas pendientes se quedan.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="destructive"
            size={size}
            onClick={() => {
              onConfirm()
              setOpen(false)
            }}
          >
            Borrar
          </Button>
          <Button
            ref={cancelRef}
            variant="secondary"
            size={size}
            onClick={() => setOpen(false)}
          >
            Cancelar
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

/** The muted line under the header when the model is not the one answering. */
function engineNote(provider: ChatProvider): string | null {
  switch (provider.engine) {
    case 'ai':
      return null
    case 'budget_exhausted':
      // Waiting does not refill a budget, so this does not say "try again later".
      return 'Se agotó el presupuesto del asistente.'
    case 'degraded':
      return 'El asistente no responde. Intenta otra vez en un momento.'
    case 'unconfigured':
    default:
      return 'El asistente no está disponible por ahora.'
  }
}

// ---------------------------------------------------------------------------
// Transcript
// ---------------------------------------------------------------------------

type Group =
  | { kind: 'user'; id: string; text: string }
  | {
      kind: 'reply'
      id: string
      /** Every turn folded into this reply, for finding its proposals. */
      turnIds: string[]
      texts: string[]
      tools: ToolRun[]
      errors: string[]
    }

/**
 * Folds consecutive assistant turns into one reply. A stored exchange comes back
 * as several assistant messages (one that only ran tools, one that answered);
 * on screen they are one answer with one trace.
 */
function groupTurns(turns: Turn[]): Group[] {
  const groups: Group[] = []
  for (const turn of turns) {
    if (turn.role === 'user') {
      groups.push({ kind: 'user', id: turn.id, text: turn.text ?? '' })
      continue
    }
    const last = groups.at(-1)
    const reply =
      last?.kind === 'reply'
        ? last
        : {
            kind: 'reply' as const,
            id: turn.id,
            turnIds: [],
            texts: [],
            tools: [],
            errors: [],
          }
    if (reply !== last) groups.push(reply)
    reply.turnIds.push(turn.id)
    if (turn.text) reply.texts.push(turn.text)
    if (turn.tools) reply.tools.push(...turn.tools)
    if (turn.error) reply.errors.push(turn.error)
  }
  return groups
}

/** The last group that came from the stored history, or -1 when there is none. */
function lastStoredIndex(groups: Group[]): number {
  for (let index = groups.length - 1; index >= 0; index--) {
    if (groups[index]?.id.startsWith(STORED_PREFIX)) return index
  }
  return -1
}

/**
 * Decides where each proposal is drawn: under the reply whose stream announced
 * it, or after the stored history when no reply on screen did (after a reload,
 * or one made in another tab). A proposal shows while it is pending, or applied in this session
 * (with its chip); one the stream just announced shows from the event until the
 * proposals query has refreshed past it.
 */
function placeProposals(
  groups: Group[],
  byTurn: Record<string, AnnouncedProposal[]>,
  state: {
    pending: Proposal[]
    fetchedAt: number
    applied: Record<string, Proposal>
    dismissed: Record<string, true>
    orderOf: (id: string) => number
  },
): { byGroup: Map<string, Proposal[]>; unplaced: Proposal[] } {
  const pendingById = new Map(state.pending.map((proposal) => [proposal.id, proposal]))
  const resolve = (id: string, announced?: AnnouncedProposal): Proposal | null => {
    if (state.dismissed[id]) return null
    const known = state.applied[id] ?? pendingById.get(id)
    if (known) return known
    if (announced && announced.at > state.fetchedAt) {
      return {
        id,
        kind: announced.kind,
        summary: announced.summary,
        status: 'pending',
        expires_at: '',
      }
    }
    return null
  }

  const placedIds = new Set<string>()
  const byGroup = new Map<string, Proposal[]>()
  for (const group of groups) {
    if (group.kind !== 'reply') continue
    const list: Proposal[] = []
    for (const turnId of group.turnIds) {
      for (const announced of byTurn[turnId] ?? []) {
        if (placedIds.has(announced.id)) continue
        placedIds.add(announced.id)
        const proposal = resolve(announced.id, announced)
        if (proposal) list.push(proposal)
      }
    }
    if (list.length > 0) byGroup.set(group.id, list)
  }

  // The order each was first seen in (the server lists them oldest first), so a
  // card that gets applied keeps its place instead of jumping to the end.
  const unplaced = [...state.pending, ...Object.values(state.applied)]
    .filter(
      (proposal, index, all) =>
        !placedIds.has(proposal.id) &&
        !state.dismissed[proposal.id] &&
        all.findIndex((other) => other.id === proposal.id) === index,
    )
    .map((proposal) => ({
      proposal: state.applied[proposal.id] ?? proposal,
      // Asked in list order, so a proposal seen for the first time is placed
      // where the server lists it.
      position: state.orderOf(proposal.id),
    }))
    .sort((a, b) => a.position - b.position)
    .map(({ proposal }) => proposal)

  return { byGroup, unplaced }
}

function Question({ text }: { text: string }) {
  return (
    <Message align="end">
      <MessageContent>
        <Bubble variant="default" align="end">
          <BubbleContent>
            <span className="sr-only">Tú: </span>
            {text}
          </BubbleContent>
        </Bubble>
      </MessageContent>
    </Message>
  )
}

type ReplyGroup = Extract<Group, { kind: 'reply' }>

function isEmpty(group: ReplyGroup): boolean {
  return group.tools.length === 0 && group.texts.length === 0 && group.errors.length === 0
}

function Reply({ group }: { group: ReplyGroup }) {
  if (isEmpty(group)) return null
  const trace = traceOf(group.tools)

  return (
    <Message align="start">
      <MessageContent className="gap-2">
        <span className="sr-only">Asistente:</span>
        {trace.length > 0 && (
          <p className="text-small text-muted-foreground">
            <span className="sr-only">Lo que revisó: </span>
            {trace.join(' · ')}
          </p>
        )}
        {group.texts.length > 0 && (
          <div className="flex flex-col gap-3 text-body text-pretty text-foreground">
            {renderReply(group.texts.join('\n\n'))}
          </div>
        )}
        {group.errors.map((error, index) => (
          <p key={index} className="flex gap-2 text-small text-attention">
            <CircleAlertIcon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
            {error}
          </p>
        ))}
      </MessageContent>
    </Message>
  )
}

/** What the assistant ran, in the customer's words, each named once. */
function traceOf(tools: ToolRun[]): string[] {
  const labels: string[] = []
  for (const tool of tools) {
    const label = `${TOOL_LABELS[tool.name] ?? 'Consultó tus datos'}${tool.failed ? ' (no funcionó)' : ''}`
    if (!labels.includes(label)) labels.push(label)
  }
  return labels
}

function Working({ tool }: { tool: string | null }) {
  return (
    <p role="status" className="flex items-center gap-2 text-small text-muted-foreground">
      <span aria-hidden="true" className="flex gap-1">
        {[0, 1, 2].map((index) => (
          <span
            key={index}
            className="size-1.5 animate-pulse rounded-full bg-muted-foreground"
            style={{ animationDelay: `${index * 160}ms` }}
          />
        ))}
      </span>
      {tool ? `${TOOL_LABELS[tool] ?? 'Revisando tus datos'}…` : 'Pensando…'}
    </p>
  )
}

/** The three suggestions, styled as filter chips. Tapping one asks it. */
function SuggestionChips({
  variant,
  items,
  disabled,
  scroll,
  onPick,
}: {
  variant: Variant
  items: string[]
  disabled?: boolean
  /** One row that scrolls sideways, bleeding to the sheet's edges. */
  scroll?: boolean
  onPick: (text: string) => void
}) {
  return (
    <ul
      aria-label="Sugerencias"
      className={cn(
        'flex gap-2',
        scroll ? 'no-scrollbar -mx-4 overflow-x-auto px-4' : 'flex-wrap',
      )}
    >
      {items.map((item) => (
        <li key={item} className="shrink-0">
          <button
            type="button"
            disabled={disabled}
            onClick={() => onPick(item)}
            className={cn(
              'inline-flex items-center rounded-full bg-muted px-4 text-small font-semibold whitespace-nowrap text-foreground',
              'transition-colors duration-180 ease-out hover:bg-line',
              'disabled:cursor-not-allowed disabled:text-muted-foreground disabled:hover:bg-muted',
              variant === 'sheet' ? 'h-11' : 'h-9',
            )}
          >
            {item}
          </button>
        </li>
      ))}
    </ul>
  )
}

function ChatSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-hidden="true">
      <Skeleton className="h-11 w-56 self-end" />
      <div className="flex flex-col gap-2">
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-1/2" />
      </div>
    </div>
  )
}
