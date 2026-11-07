import { request, requestFile, requestUpload } from './client'
import type {
  AccountList,
  Account,
  Capture,
  CaptureReviewItem,
  CategoryNode,
  CategoryRule,
  CategoryTotal,
  ChatHistory,
  Checkpoint,
  ClaudeConnection,
  ConnectAnswer,
  ConnectRequest,
  DeviceStatus,
  EntryKind,
  EntryPage,
  FlowPoint,
  Proposal,
  Entry,
  ImportResult,
  ImportRun,
  InvestmentLinkStatus,
  InvestmentSyncResult,
  InvestmentTrade,
  McpResource,
  Me,
  MonthPlan,
  NewShortcutKey,
  PlanCapLine,
  PlanItem,
  PlanItemKind,
  PlanLink,
  PlanScope,
  PlanSuggestion,
  PlanSuggestions,
  Portfolio,
  Reconciliation,
  RuleReach,
  SecurityStatus,
  Session,
  StatementFileReport,
  StatementUpload,
  StatementUploadStatus,
  TransferSuggestion,
} from './types'

/**
 * One function per endpoint, so a URL or a query-string name appears exactly once
 * in the app. Everything below is a thin wrapper — the interesting behaviour is in
 * the client (refresh, streaming) and in the hooks (caching, invalidation).
 */

export const login = (email: string, password: string) =>
  request<Session>('/api/auth/login', { method: 'POST', body: { email, password } })

export const register = (input: { email: string; password: string; full_name: string }) =>
  request<Session>('/api/auth/register', { method: 'POST', body: input })

export const logout = () => request<void>('/api/auth/logout', { method: 'POST' })

export const loginPin = (pin: string) =>
  request<Session>('/api/auth/login-pin', { method: 'POST', body: { pin } })

/**
 * Asks, before anything is typed, whether this browser is trusted for PIN
 * login. Never throws on "not trusted" — that is the ordinary `{ trusted:
 * false }` response, not a failure.
 */
export const fetchDeviceStatus = () => request<DeviceStatus>('/api/auth/device')

// --- security (PIN) -----------------------------------------------------

export const fetchSecurityStatus = () =>
  request<SecurityStatus>('/api/auth/security/status')

export const setPin = (currentPassword: string, pin: string) =>
  request<void>('/api/auth/security/pin', {
    method: 'POST',
    body: { current_password: currentPassword, pin },
  })

export const removePin = (currentPassword: string) =>
  request<void>('/api/auth/security/pin', {
    method: 'DELETE',
    body: { current_password: currentPassword },
  })

export const enableDeviceForPin = () =>
  request<SecurityStatus>('/api/auth/security/pin/device', { method: 'POST' })

export const disableDeviceForPin = () =>
  request<void>('/api/auth/security/pin/device', { method: 'DELETE' })

// --- accounts ---------------------------------------------------------------

// --- accounts ---------------------------------------------------------------

export const fetchMe = () => request<Me>('/api/me')

export const fetchAccounts = () => request<AccountList>('/api/accounts')

export const fetchAccount = (id: string) =>
  request<Account>(`/api/accounts/${encodeURIComponent(id)}`)

/**
 * Adds a card someone else pays, the one account the owner makes by hand: what they
 * call it and its last four digits. Answers with the new account (201). The form
 * chooses the `id`, so sending the same card again (an answer lost on the way)
 * answers with the card the first send added. A 409 when they already have another
 * one with those digits, or import the statements of a card that has them; a 422
 * with `fields` (alias, last4, both at once) for what is missing.
 */
export const addOthersCard = (input: { id: string; alias: string; last4: string }) =>
  request<Account>('/api/accounts', { method: 'POST', body: input })

/** Renames an account, or relabels a bank account as checking or savings. */
export const updateAccount = (
  id: string,
  patch: { alias?: string; type?: 'checking' | 'savings' },
) =>
  request<Account>(`/api/accounts/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: patch,
  })

/** Deletes an account and everything imported into it. */
export const deleteAccount = (id: string) =>
  request<void>(`/api/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' })

export const fetchCheckpoints = (accountId: string) =>
  request<{ checkpoints: Checkpoint[] }>(
    `/api/accounts/${encodeURIComponent(accountId)}/checkpoints`,
  )

export interface CheckpointInput {
  as_of: string
  /** Signed from the owner's view: a card owing $14.30 is "-14.30". */
  balance: string
  pin?: boolean
  due_on?: string
  minimum_payment?: string
  note?: string
}

export const addCheckpoint = (accountId: string, input: CheckpointInput) =>
  request<Checkpoint>(`/api/accounts/${encodeURIComponent(accountId)}/checkpoints`, {
    method: 'POST',
    body: input,
  })

export const deleteCheckpoint = (accountId: string, checkpointId: string) =>
  request<void>(
    `/api/accounts/${encodeURIComponent(accountId)}/checkpoints/${encodeURIComponent(checkpointId)}`,
    { method: 'DELETE' },
  )

/** Makes a checkpoint the anchor, or with null lets the earliest one anchor. */
export const pinCheckpoint = (accountId: string, checkpointId: string | null) =>
  request<Account>(`/api/accounts/${encodeURIComponent(accountId)}/anchor`, {
    method: 'PUT',
    body: { checkpoint_id: checkpointId },
  })

export const fetchReconciliation = (accountId: string) =>
  request<Reconciliation>(`/api/accounts/${encodeURIComponent(accountId)}/reconciliation`)

// --- movements --------------------------------------------------------------

export interface EntryQuery {
  accountId?: string
  kinds?: EntryKind[]
  from?: string
  to?: string
  search?: string
  categoryId?: string
  uncategorized?: boolean
  limit?: number
  cursor?: string
}

export function entryParams(query: EntryQuery): URLSearchParams {
  const params = new URLSearchParams()
  if (query.accountId) params.append('account_id', query.accountId)
  for (const kind of query.kinds ?? []) params.append('kind', kind)
  if (query.from) params.set('from', query.from)
  if (query.to) params.set('to', query.to)
  if (query.search) params.set('search', query.search)
  if (query.categoryId) params.append('category_id', query.categoryId)
  if (query.uncategorized) params.set('uncategorized', 'true')
  if (query.limit) params.set('limit', String(query.limit))
  if (query.cursor) params.set('cursor', query.cursor)
  return params
}

export function fetchEntries(query: EntryQuery = {}) {
  const qs = entryParams(query).toString()
  return request<EntryPage>(`/api/entries${qs ? `?${qs}` : ''}`)
}

export interface EntryPatch {
  category_id?: string | null
  note?: string
  /** Marks a movement as a transfer between the owner's own accounts, or undoes it. */
  transfer?: boolean
}

export const updateEntry = (id: string, patch: EntryPatch) =>
  request<Entry>(`/api/entries/${encodeURIComponent(id)}`, { method: 'PATCH', body: patch })

/** Downloads the movements matching a query as a CSV file, every page of them. */
export function exportEntriesCSV(query: EntryQuery = {}) {
  const qs = entryParams({ ...query, limit: undefined, cursor: undefined }).toString()
  return requestFile(`/api/entries/export.csv${qs ? `?${qs}` : ''}`)
}

// --- noted purchases --------------------------------------------------------

export interface CaptureInput {
  /**
   * Chosen by the form, the same on every try: sending it again returns the purchase
   * the first try noted instead of noting it twice.
   */
  id?: string
  account_id: string
  /** What was paid, positive and as typed: "12.50". */
  amount: string
  /** Where: "Super 99". */
  merchant: string
  /** The purchase's day (YYYY-MM-DD); today in Panama when left out. */
  day?: string
  /** Left out, the owner's rules file it by merchant. */
  category_id?: string
  note?: string
}

/**
 * Notes a purchase by hand. It comes back with its state: `linked` or `printed` when
 * its line was already imported, so it does not count twice.
 */
export const addCapture = (input: CaptureInput) =>
  request<Capture>('/api/captures', { method: 'POST', body: input })

/** Corrects a noted purchase; a field left out stays as it is. */
export interface CapturePatch {
  /** Positive, as in CaptureInput. */
  amount?: string
  merchant?: string
  day?: string
}

export const editCapture = (id: string, patch: CapturePatch) =>
  request<Capture>(`/api/captures/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: patch,
  })

/** Discards a noted purchase that never happened, so it stops counting. */
export const discardCapture = (id: string) =>
  request<Capture>(`/api/captures/${encodeURIComponent(id)}/discard`, { method: 'PUT' })

/** Takes a discarded purchase back. */
export const restoreCapture = (id: string) =>
  request<Capture>(`/api/captures/${encodeURIComponent(id)}/discard`, { method: 'DELETE' })

/** Unlinks a noted purchase from its line, which is remembered as not being it. */
export const unlinkCapture = (id: string) =>
  request<Capture>(`/api/captures/${encodeURIComponent(id)}/link`, { method: 'DELETE' })

/**
 * Links a noted purchase to the printed line the owner says it is. Any amount goes:
 * a tip is why the two can differ.
 */
export const linkCapture = (id: string, entryId: string) =>
  request<Capture>(`/api/captures/${encodeURIComponent(id)}/link`, {
    method: 'POST',
    body: { entry_id: entryId },
  })

/** Says a printed line is not this noted purchase: it is never offered for it again. */
export const rejectCaptureLine = (id: string, entryId: string) =>
  request<Capture>(`/api/captures/${encodeURIComponent(id)}/reject`, {
    method: 'POST',
    body: { entry_id: entryId },
  })

/** The noted purchases the owner should look at, newest first. */
export const fetchCaptureReview = () =>
  request<{ captures: CaptureReviewItem[] }>('/api/captures/review')

// --- the phone's shortcut -------------------------------------------------------

/**
 * Where the phone's shortcut sends each purchase, with the account's key. The app
 * never calls it; it shows it, after this site's origin, for the owner to paste.
 */
export const SHORTCUT_PURCHASES_PATH = '/api/shortcut/purchases'

/**
 * Makes the account's shortcut key, turning off the one it had. The answer is the
 * only time the key itself is ever sent: see useCreateShortcutKey.
 */
export const createShortcutKey = (accountId: string) =>
  request<NewShortcutKey>(`/api/captures/keys/${encodeURIComponent(accountId)}`, {
    method: 'POST',
  })

/** Turns the account's shortcut key off: its shortcut stops noting purchases at once. */
export const revokeShortcutKey = (accountId: string) =>
  request<void>(`/api/captures/keys/${encodeURIComponent(accountId)}`, {
    method: 'DELETE',
  })

// --- reports -------------------------------------------------------------------

export const fetchCategoryTotals = (query: {
  from?: string
  to?: string
  kind?: 'spend' | 'income'
}) => {
  const params = new URLSearchParams()
  if (query.from) params.set('from', query.from)
  if (query.to) params.set('to', query.to)
  if (query.kind) params.set('kind', query.kind)
  return request<{ totals: CategoryTotal[] }>(`/api/reports/categories?${params}`)
}

export const fetchFlow = (query: {
  from?: string
  to?: string
  granularity?: 'day' | 'month'
}) => {
  const params = new URLSearchParams()
  if (query.from) params.set('from', query.from)
  if (query.to) params.set('to', query.to)
  if (query.granularity) params.set('granularity', query.granularity)
  return request<{ points: FlowPoint[] }>(`/api/reports/flow?${params}`)
}

// --- transfers -----------------------------------------------------------------

export const fetchTransferSuggestions = () =>
  request<{ suggestions: TransferSuggestion[] }>('/api/transfers/suggestions')

export const decideTransfer = (input: {
  out_entry_id: string
  in_entry_id: string
  confirm: boolean
}) => request<void>('/api/transfers/decisions', { method: 'POST', body: input })

// --- the month's plan ------------------------------------------------------------

/** The plan of a month (YYYY-MM); without one, the current month in Panama. */
export const fetchPlan = (month?: string) =>
  request<MonthPlan>(`/api/plan${month ? `?month=${encodeURIComponent(month)}` : ''}`)

/**
 * An item as a form saves it, for `month` on (`scope: 'from'`) or for it alone
 * (`'only'`). Amounts travel as typed text ("650.00"). Only these fields: the API
 * refuses one it does not know.
 */
export interface PlanItemInput {
  kind: Exclude<PlanItemKind, 'purchase'>
  month: string
  scope: PlanScope
  name: string
  amount: string
  /** 1 to 31; a paycheck and a payment need one. */
  day?: number
  match_text?: string
  /** null or left out: no category. */
  category_id?: string | null
  /** A saving's account; null or left out: none. */
  account_id?: string | null
  /** A saving's goal; left out: none. */
  target?: string
  target_month?: string
}

/**
 * Creates the item with the caller's id (201) or edits it (200), and answers with
 * the month it was saved for. Sending the same again changes nothing, which is how
 * an edit is undone: the previous values, sent again.
 */
export const savePlanItem = (id: string, input: PlanItemInput) =>
  request<PlanItem>(`/api/plan/items/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: input,
  })

/** Takes `month` on, or that month alone, out of an item; one left with no month goes. */
export const deletePlanItem = (id: string, month: string, scope: PlanScope) =>
  request<void>(
    `/api/plan/items/${encodeURIComponent(id)}?${new URLSearchParams({ month, scope })}`,
    { method: 'DELETE' },
  )

/** A cap on an expense category, for `month` on or for it alone. */
export interface PlanCapInput {
  month: string
  scope: PlanScope
  amount: string
}

export const savePlanCap = (categoryId: string, input: PlanCapInput) =>
  request<Omit<PlanCapLine, 'spent' | 'level'>>(
    `/api/plan/caps/${encodeURIComponent(categoryId)}`,
    { method: 'PUT', body: input },
  )

export const deletePlanCap = (categoryId: string, month: string, scope: PlanScope) =>
  request<void>(
    `/api/plan/caps/${encodeURIComponent(categoryId)}?${new URLSearchParams({ month, scope })}`,
    { method: 'DELETE' },
  )

/**
 * What the owner says about one month of a paycheck or a payment (a month that has
 * passed too): this movement paid it (`assign`), this one did not (`reject`), or it
 * was paid, or came in, where chenchén cannot see (`paid`, with no movement).
 */
export type PlanLinkSaying = { item_id: string; month: string } & (
  { decision: 'assign' | 'reject'; movement_id: string } | { decision: 'paid' }
)

/** A saying with the caller's id, so a send whose answer was lost is safe to repeat. */
export type PlanLinkInput = PlanLinkSaying & { id: string }

/**
 * 201 with the link it records, or 200 with the one already there: the same id, or
 * the same word about the same movement and month. That one may have another id, so
 * taking it back uses the id in the answer. Saying the opposite about a movement
 * replaces what was said. Refusals, each with the server's sentence:
 *   409 movement_in_plan    the movement already pays another item or month
 *   422 movement_too_far    it is booked more than a month away from that month
 *   404 entry_not_found     the movement is gone (a noted purchase discarded)
 *   404 plan_item_not_found the item is not planned for that month
 *   409 plan_link_id_taken  the id is someone else's: a new one goes through
 */
export const addPlanLink = (input: PlanLinkInput) =>
  request<PlanLink>('/api/plan/links', { method: 'POST', body: input })

/**
 * Takes back what a link said: the month is recognised again as if it had never been
 * said. 404 plan_link_not_found when it was taken back already, or went with its
 * movement or its item's month.
 */
export const deletePlanLink = (id: string) =>
  request<void>(`/api/plan/links/${encodeURIComponent(id)}`, { method: 'DELETE' })

/**
 * The paychecks, payments and savings the owner's history suggests planning, that
 * the plan does not have yet and the owner has not turned down.
 */
export const fetchPlanSuggestions = () => request<PlanSuggestions>('/api/plan/suggestions')

/** A suggestion as a dismissal names it: its kind and its key, as the server gave them. */
export type PlanDismissal = Pick<PlanSuggestion, 'kind' | 'key'>

/** Turns a suggestion down: it is not offered again. Saying it twice is saying it once. */
export const dismissPlanSuggestion = ({ kind, key }: PlanDismissal) =>
  request<void>('/api/plan/dismissals', { method: 'PUT', body: { kind, key } })

/**
 * Takes turning it down back: it is offered again while the history suggests it.
 * The key travels in the query, where a paycheck's "#1" has to be encoded.
 */
export const undismissPlanSuggestion = ({ kind, key }: PlanDismissal) =>
  request<void>(`/api/plan/dismissals?${new URLSearchParams({ kind, key })}`, {
    method: 'DELETE',
  })

// --- rules ---------------------------------------------------------------------

export interface RuleInput {
  match_text: string
  category_id?: string | null
  transfer: boolean
  priority?: number
  apply_to_existing?: boolean
}

export const fetchRules = () => request<{ rules: CategoryRule[] }>('/api/category-rules')

export const createRule = (input: RuleInput) =>
  request<CategoryRule>('/api/category-rules', { method: 'POST', body: input })

export const updateRule = (id: string, input: RuleInput) =>
  request<CategoryRule>(`/api/category-rules/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: input,
  })

export const deleteRule = (id: string) =>
  request<void>(`/api/category-rules/${encodeURIComponent(id)}`, { method: 'DELETE' })

/** What a rule with this text would reach, before it exists. */
export const fetchRuleReach = (text: string) =>
  request<RuleReach>(`/api/category-rules/preview?${new URLSearchParams({ text })}`)

// --- proposals -----------------------------------------------------------------

export const fetchProposals = () => request<{ proposals: Proposal[] }>('/api/proposals')

export const applyProposal = (id: string) =>
  request<Proposal>(`/api/proposals/${encodeURIComponent(id)}/apply`, { method: 'POST' })

export const rejectProposal = (id: string) =>
  request<Proposal>(`/api/proposals/${encodeURIComponent(id)}/reject`, { method: 'POST' })

// --- chat -------------------------------------------------------------------

export const fetchChatHistory = () => request<ChatHistory>('/api/chat')

export const clearChat = () => request<void>('/api/chat', { method: 'DELETE' })

// --- investments -------------------------------------------------------------

/** Links an IBKR account, creating its brokerage account on first sight. */
export const linkIBKR = (input: {
  ibkr_account_id: string
  flex_query_id: string
  flex_token: string
}) =>
  request<{ account_id: string }>('/api/investments/link', { method: 'POST', body: input })

export const relinkIBKR = (
  accountId: string,
  input: { flex_query_id: string; flex_token: string },
) =>
  request<void>(`/api/investments/accounts/${encodeURIComponent(accountId)}/link`, {
    method: 'PUT',
    body: input,
  })

export const fetchInvestmentLink = (accountId: string) =>
  request<InvestmentLinkStatus>(
    `/api/investments/accounts/${encodeURIComponent(accountId)}/link`,
  )

export const syncInvestmentAccount = (accountId: string) =>
  request<InvestmentSyncResult>(
    `/api/investments/accounts/${encodeURIComponent(accountId)}/sync`,
    { method: 'POST' },
  )

export const fetchPortfolio = (accountId: string) =>
  request<Portfolio>(`/api/investments/accounts/${encodeURIComponent(accountId)}/portfolio`)

export const fetchInvestmentTrades = (accountId: string, limit?: number) =>
  request<{ trades: InvestmentTrade[] }>(
    `/api/investments/accounts/${encodeURIComponent(accountId)}/trades${limit ? `?limit=${limit}` : ''}`,
  )

// --- imports -----------------------------------------------------------------

/**
 * Imports a statement file. Given an account, a file for any other account is
 * refused instead of being filed elsewhere.
 */
export const importStatement = (file: File, accountId?: string) =>
  requestUpload<ImportResult>(
    '/api/imports',
    file,
    accountId ? { account_id: accountId } : undefined,
  )

/** Parses a statement file and reports what it says, without importing it. */
export const dryRunStatement = (file: File) =>
  requestUpload<StatementFileReport>('/api/imports/dry-run', file)

export const fetchImportRuns = (accountId: string) =>
  request<{ runs: ImportRun[] }>(`/api/imports?account_id=${encodeURIComponent(accountId)}`)

// --- statements Claude sent -----------------------------------------------------

/** The statements Claude sent that are in one state; by default, those waiting. */
export const fetchStatementUploads = (status: StatementUploadStatus = 'needs_review') =>
  request<{ uploads: StatementUpload[] }>(
    `/api/imports/uploads?status=${encodeURIComponent(status)}`,
  )

/** What the file a waiting upload holds says, without importing it: a dry run. */
export const dryRunStatementUpload = (id: string) =>
  request<StatementFileReport>(`/api/imports/uploads/${encodeURIComponent(id)}/dry-run`)

/**
 * Imports the file a waiting upload holds, as the web's own import does: a new
 * account is made the first time its file is seen. The file is dropped once it is in.
 */
export const importStatementUpload = (id: string) =>
  request<ImportResult>(`/api/imports/uploads/${encodeURIComponent(id)}/import`, {
    method: 'POST',
  })

/** Sets a waiting upload aside and drops its file. */
export const dismissStatementUpload = (id: string) =>
  request<void>(`/api/imports/uploads/${encodeURIComponent(id)}/dismiss`, {
    method: 'POST',
  })

// --- Claude ---------------------------------------------------------------------

/**
 * The MCP endpoint's protected-resource metadata (RFC 9728). Its `resource` is where
 * Claude reaches chenchén's tools, which the app shows for the owner to add as a
 * connector. At the root of the domain, not under /api.
 */
export const fetchMcpResource = () =>
  request<McpResource>('/.well-known/oauth-protected-resource/api/mcp')

/**
 * A connection Claude asked for. Opening it makes it this owner's to answer; a
 * 404 (oauth_request_not_found) when it expired, was answered or is someone else's.
 */
export const fetchConnectRequest = (id: string) =>
  request<ConnectRequest>(`/api/oauth/requests/${encodeURIComponent(id)}`)

/** Yes, with whether Claude may also write. The answer says where to go next. */
export const approveConnectRequest = (id: string, write: boolean) =>
  request<ConnectAnswer>(`/api/oauth/requests/${encodeURIComponent(id)}/approve`, {
    method: 'POST',
    body: { write },
  })

/** No. The answer says where to go next: back to Claude, which hears the no. */
export const denyConnectRequest = (id: string) =>
  request<ConnectAnswer>(`/api/oauth/requests/${encodeURIComponent(id)}/deny`, {
    method: 'POST',
  })

/** The owner's live connections with Claude, newest first. */
export const fetchClaudeConnections = () =>
  request<{ grants: ClaudeConnection[] }>('/api/oauth/grants')

/** Ends a connection and every token it issued: Claude has to connect again. */
export const disconnectClaude = (id: string) =>
  request<void>(`/api/oauth/grants/${encodeURIComponent(id)}`, { method: 'DELETE' })

// --- categories ---------------------------------------------------------------

export const fetchCategories = () =>
  request<{ categories: CategoryNode[] }>('/api/categories')
