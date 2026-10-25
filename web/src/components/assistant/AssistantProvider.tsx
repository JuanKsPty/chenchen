import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { ApiError, streamChat } from '@/api/client'
import type { ChatEvent, ChatProvider, Proposal } from '@/api/types'
import { keys, useChatHistory, useClearChat } from '@/lib/queries'

/**
 * The assistant: its open state and its conversation, owned above the layout.
 *
 * Frozen interface (F6). Pages and the shell rely on exactly this:
 *
 *   const { open, setOpen, toggle, ask, send, clear, turns } = useAssistant()
 *
 *   open           whether the assistant is showing: the desktop panel or the mobile
 *                  sheet, whichever the viewport uses. Starts false on every visit and
 *                  is never stored.
 *   setOpen(v)     opens or closes it.
 *   toggle()       the desktop bar's Asistente button.
 *   ask(text)      opens the assistant and sends `text` as the next question, for a
 *                  button that asks on the owner's behalf ("Pregúntale al asistente").
 *                  While a reply is still streaming it only opens and leaves `text` in
 *                  the composer.
 *   send(text?)    sends `text`, or the composer's draft. Does not open anything.
 *   clear()        deletes the conversation.
 *   turns          the transcript, oldest first.
 *
 * Also exposed for the panel: draft, setDraft, streaming, activeTool, failure,
 * dismissFailure, clearing, provider, loadingHistory.
 *
 * Proposals (W6, separate from the frozen value above): useAssistantProposals()
 * gives the panel which live turn announced which proposal (the stream's
 * `proposal` events), and the ones decided in this session, so a card can sit
 * right under the reply that made it and keep its "Aplicado" chip after the
 * server stops listing it.
 *
 * The conversation itself:
 *
 * This state used to live inside the panel component, which was fine while the panel
 * existed in exactly one place. It no longer does: on a wide screen the transcript is
 * a panel beside the page, and on a phone it is a full-screen sheet.
 * Had the state stayed in the view, those two would each own a copy — two `turns`
 * arrays, and worse, two `streamChat` connections writing into different state, with
 * a resize across the breakpoint tearing down whichever one was mid-answer.
 *
 * So the stream lives here, above the fork, and both surfaces are pure renderers of
 * it. Mounting, unmounting or duplicating a view is now free, and a reply that
 * arrives while the sheet is shut is still there when it opens.
 */

export interface ToolRun {
  name: string
  failed?: boolean
}

export interface Turn {
  id: string
  role: 'user' | 'assistant'
  text?: string
  tools?: ToolRun[]
  /** Set on a turn that failed, so the message is styled as a failure. */
  error?: string
}

/**
 * What each tool did, in the customer's words.
 *
 * The trace is shown, not hidden. When software can move your money, seeing exactly
 * which operations it performed is not developer detail — it is the audit trail, and
 * it is the reason to trust the answer above it.
 */
export const TOOL_LABELS: Record<string, string> = {
  list_accounts: 'Consultó tus cuentas',
  list_movements: 'Consultó tus movimientos',
  list_categories: 'Consultó tus categorías',
  spending_by_category: 'Sumó tus gastos por categoría',
  cash_flow: 'Revisó tus entradas y salidas',
  explain_drift: 'Revisó por qué no cuadra',
  list_transfer_suggestions: 'Buscó transferencias sin confirmar',
  get_portfolio: 'Consultó tus inversiones',
  list_rules: 'Consultó tus reglas',
  propose_recategorize: 'Propuso una categoría',
  propose_note: 'Propuso una nota',
  propose_rule: 'Propuso una regla',
  propose_transfer_decision: 'Propuso una transferencia',
  propose_checkpoint: 'Propuso un saldo',
}

interface AssistantContextValue {
  turns: Turn[]
  draft: string
  setDraft: (value: string) => void
  streaming: boolean
  activeTool: string | null
  failure: string | null
  dismissFailure: () => void
  /** Sends `text`, or the current draft when called without one. */
  send: (text?: string) => void
  clear: () => void
  clearing: boolean
  provider?: ChatProvider
  loadingHistory: boolean
  /**
   * Whether the assistant is showing (the desktop panel or the mobile sheet). Closed by
   * default and never remembered: the page is the point, the assistant is one tap away.
   */
  open: boolean
  setOpen: (value: boolean) => void
  /** Opens it when closed, closes it when open. */
  toggle: () => void
  /** Opens the assistant and asks `text`. */
  ask: (text: string) => void
}

const AssistantContext = createContext<AssistantContextValue | null>(null)

export function useAssistant(): AssistantContextValue {
  const value = useContext(AssistantContext)
  if (!value) throw new Error('useAssistant must be used inside an AssistantProvider')
  return value
}

/** The id prefix of turns loaded from the stored history, as opposed to live ones. */
export const STORED_PREFIX = 'stored-'

/** A proposal as the stream announced it, before the proposals query has caught up. */
export interface AnnouncedProposal {
  id: string
  kind: Proposal['kind']
  summary: string
  /** When it arrived (ms since epoch), compared with the query's last fetch. */
  at: number
}

interface ProposalStateValue {
  /** The proposals each live assistant turn produced, by turn id, in order. */
  byTurn: Record<string, AnnouncedProposal[]>
  /**
   * Applied in this session. The server stops listing a proposal once it is
   * decided, so this is what keeps the card and its "Aplicado" chip on screen.
   */
  applied: Record<string, Proposal>
  /** Discarded in this session: hidden at once, before the list refreshes. */
  dismissed: Record<string, true>
  markApplied: (proposal: Proposal) => void
  markDismissed: (id: string) => void
  /**
   * A stable position for a proposal, given the first time it is asked for, so
   * the cards keep their order when one is applied and leaves the server's list.
   */
  orderOf: (id: string) => number
}

const ProposalStateContext = createContext<ProposalStateValue | null>(null)

/** Where the assistant's proposals go in the transcript, and which were decided. */
export function useAssistantProposals(): ProposalStateValue {
  const value = useContext(ProposalStateContext)
  if (!value) {
    throw new Error('useAssistantProposals must be used inside an AssistantProvider')
  }
  return value
}

export function AssistantProvider({ children }: { children: React.ReactNode }) {
  const history = useChatHistory()
  const clearMutation = useClearChat()
  const queryClient = useQueryClient()

  const [turns, setTurns] = useState<Turn[]>([])
  const [draft, setDraft] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [activeTool, setActiveTool] = useState<string | null>(null)
  const [failure, setFailure] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  /**
   * The engine reported by the last exchange, which supersedes the one the history
   * endpoint gave on load.
   *
   * The two can disagree by the time somebody has sent a message: the AI budget is
   * finite and shared, so it can run out mid-session. Keeping the label on the
   * load-time value would attribute an unavailable notice to a model.
   */
  const [liveProvider, setLiveProvider] = useState<ChatProvider | null>(null)
  const [byTurn, setByTurn] = useState<Record<string, AnnouncedProposal[]>>({})
  const [applied, setApplied] = useState<Record<string, Proposal>>({})
  const [dismissed, setDismissed] = useState<Record<string, true>>({})

  const abortRef = useRef<AbortController | null>(null)
  const orderRef = useRef(new Map<string, number>())
  const hydratedRef = useRef(false)

  // The stored conversation is loaded once, into the same list the live stream
  // appends to. Loading it on every render would fight the stream for the same state.
  useEffect(() => {
    if (hydratedRef.current || !history.data) return
    hydratedRef.current = true

    setTurns(
      history.data.messages.map((message) => ({
        id: `${STORED_PREFIX}${message.id}`,
        role: message.role === 'user' ? 'user' : 'assistant',
        text: message.text,
        tools: message.tools?.map((name) => ({ name })),
      })),
    )
  }, [history.data])

  // A stream left running after the provider unmounts — a sign-out — would keep
  // writing into state that no longer exists.
  useEffect(() => () => abortRef.current?.abort(), [])

  const send = useCallback(
    (text?: string) => {
      const message = (text ?? draft).trim()
      if (!message || streaming) return

      setDraft('')
      setFailure(null)
      setStreaming(true)

      const turnId = crypto.randomUUID()
      setTurns((previous) => [
        ...previous,
        { id: `${turnId}-user`, role: 'user', text: message },
        { id: turnId, role: 'assistant' },
      ])

      const controller = new AbortController()
      abortRef.current = controller

      const update = (change: (turn: Turn) => Turn) =>
        setTurns((previous) =>
          previous.map((turn) => (turn.id === turnId ? change(turn) : turn)),
        )

      const announce = (proposal: AnnouncedProposal) =>
        setByTurn((previous) => ({
          ...previous,
          [turnId]: [
            ...(previous[turnId] ?? []).filter((known) => known.id !== proposal.id),
            proposal,
          ],
        }))

      void (async () => {
        try {
          for await (const event of streamChat(message, controller.signal)) {
            applyEvent(event, update, setActiveTool, setLiveProvider, announce)
          }
        } catch (error) {
          if (!controller.signal.aborted) {
            setFailure(
              error instanceof ApiError
                ? error.message
                : 'Se perdió la conexión con el asistente. Intenta otra vez.',
            )
          }
        } finally {
          setStreaming(false)
          setActiveTool(null)
          abortRef.current = null
          // The assistant never writes; what changed is the history and, if it
          // proposed something, the proposals waiting for the owner.
          void queryClient.invalidateQueries({ queryKey: keys.chat })
          void queryClient.invalidateQueries({ queryKey: keys.proposals })
        }
      })()
    },
    [draft, streaming, queryClient],
  )

  const clear = useCallback(() => {
    clearMutation.mutate(undefined, {
      onSuccess: () => {
        setTurns([])
        setByTurn({})
        setApplied({})
        setDismissed({})
        orderRef.current.clear()
      },
      // The confirmation has already closed, so a failure needs saying somewhere.
      onError: () => toast('No pudimos borrar la conversación. Intenta otra vez.'),
    })
  }, [clearMutation])

  const markApplied = useCallback(
    (proposal: Proposal) =>
      setApplied((previous) => ({
        ...previous,
        [proposal.id]: { ...proposal, status: 'applied' },
      })),
    [],
  )

  const markDismissed = useCallback(
    (id: string) => setDismissed((previous) => ({ ...previous, [id]: true })),
    [],
  )

  const orderOf = useCallback((id: string) => {
    const order = orderRef.current
    let position = order.get(id)
    if (position === undefined) {
      position = order.size
      order.set(id, position)
    }
    return position
  }, [])

  const proposalState = useMemo<ProposalStateValue>(
    () => ({ byTurn, applied, dismissed, markApplied, markDismissed, orderOf }),
    [byTurn, applied, dismissed, markApplied, markDismissed, orderOf],
  )

  const dismissFailure = useCallback(() => setFailure(null), [])

  const toggle = useCallback(() => setOpen((previous) => !previous), [])

  const ask = useCallback(
    (text: string) => {
      setOpen(true)
      // A question asked while the previous answer is still arriving would be dropped
      // by `send`. Leaving it in the composer keeps it one tap from being sent.
      if (streaming) setDraft(text)
      else send(text)
    },
    [streaming, send],
  )

  const value = useMemo<AssistantContextValue>(
    () => ({
      turns,
      draft,
      setDraft,
      streaming,
      activeTool,
      failure,
      dismissFailure,
      send,
      clear,
      clearing: clearMutation.isPending,
      provider: liveProvider ?? history.data?.provider,
      loadingHistory: history.isLoading,
      open,
      setOpen,
      toggle,
      ask,
    }),
    [
      turns,
      draft,
      streaming,
      activeTool,
      failure,
      dismissFailure,
      send,
      clear,
      clearMutation.isPending,
      liveProvider,
      history.data?.provider,
      history.isLoading,
      open,
      toggle,
      ask,
    ],
  )

  return (
    <AssistantContext.Provider value={value}>
      <ProposalStateContext.Provider value={proposalState}>
        {children}
      </ProposalStateContext.Provider>
    </AssistantContext.Provider>
  )
}

/** applyEvent folds one stream event into the assistant's turn. */
function applyEvent(
  event: ChatEvent,
  update: (change: (turn: Turn) => Turn) => void,
  setActiveTool: (tool: string | null) => void,
  setLiveProvider: (provider: ChatProvider) => void,
  announce: (proposal: AnnouncedProposal) => void,
) {
  switch (event.kind) {
    case 'message':
      // Turns arrive whole rather than token by token, so successive messages in one
      // exchange are joined rather than replacing each other.
      update((turn) => ({
        ...turn,
        text: turn.text ? `${turn.text}\n\n${event.text}` : event.text,
      }))
      break

    case 'tool_call':
      setActiveTool(event.tool)
      update((turn) => ({ ...turn, tools: [...(turn.tools ?? []), { name: event.tool }] }))
      break

    case 'tool_result':
      setActiveTool(null)
      update((turn) => {
        const tools = [...(turn.tools ?? [])]
        // Mark the most recent run of this tool, not the first: the same tool can be
        // called twice in one exchange.
        for (let i = tools.length - 1; i >= 0; i--) {
          if (tools[i]?.name === event.tool) {
            tools[i] = { name: event.tool, failed: event.failed }
            break
          }
        }
        return { ...turn, tools }
      })
      break

    case 'error':
      update((turn) => ({ ...turn, error: event.message }))
      break

    case 'proposal':
      // Remembered against this turn so the card shows under the reply that made
      // it. Its status still comes from the proposals query, which the end of the
      // exchange refreshes; until then the card is drawn from this event.
      announce({
        id: event.proposal.proposal_id,
        kind: event.proposal.kind as Proposal['kind'],
        summary: event.proposal.summary,
        at: Date.now(),
      })
      break

    case 'done':
      setActiveTool(null)
      // The engine that actually answered. It can have changed during this very
      // exchange — a spend ceiling reached, a key that stopped working — so the
      // label is corrected here rather than waiting for a reload.
      if (event.provider) setLiveProvider(event.provider)
      break
  }
}
