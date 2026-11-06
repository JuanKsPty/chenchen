import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'

import { ApiError } from '@/api/client'
import * as api from '@/api/endpoints'
import type {
  CaptureInput,
  CapturePatch,
  CheckpointInput,
  EntryPatch,
  EntryQuery,
  RuleInput,
} from '@/api/endpoints'

/**
 * Server state.
 *
 * One rule shapes all of it: anything that changes what the app says about money
 * — an import, a sync, a stated balance, a movement relabelled as a transfer —
 * invalidates everything that shows money. A total still showing the previous
 * figure is not a stale cache, it is a wrong number about somebody's money.
 *
 * Hooks (frozen at the F gate; a page that needs another one defines it locally
 * with useQuery, an api/endpoints function and one of these key prefixes):
 *
 *   accounts      useMe · useAccounts · useAccount(id) · useUpdateAccount(id)
 *                 ({alias?, type?: 'checking'|'savings'}) · useDeleteAccount()(id) ·
 *                 useAddOthersCard()({alias, last4}): a card someone else pays
 *   checkpoints   useCheckpoints(accountId) · useAddCheckpoint(accountId) ·
 *                 useDeleteCheckpoint(accountId)(checkpointId) ·
 *                 usePinCheckpoint(accountId)(checkpointId | null) ·
 *                 useReconciliation(accountId, {enabled?})
 *   movements     useEntries(query): one page · useEntryPages(query without cursor):
 *                 infinite, "Ver más" = fetchNextPage, a new query resets the pages ·
 *                 useUpdateEntry()({id, patch})
 *   captures      useAddCapture()(input) · useEditCapture()({id, patch}) ·
 *                 useDiscardCapture()(id): purchases the owner noted, which count as
 *                 spending until their line arrives, so each one refreshes money ·
 *                 useCaptureReview: the ones their statement does not confirm ·
 *                 useLinkCapture()({id, entryId}) · useRejectCapture()({id, entryId}) ·
 *                 useUnlinkCapture()(id)
 *   shortcut keys useCreateShortcutKey(accountId) · useRevokeShortcutKey(accountId): the
 *                 key the phone's shortcut notes purchases with. Not money: they refresh
 *                 only the accounts, whose `shortcut` says whether there is one
 *   imports       useImportStatement()({file, accountId?}) · useDryRunStatement()(file) ·
 *                 useImportRuns(accountId)
 *   investments   useInvestmentLink(accountId, {enabled?}) · useLinkIBKR ·
 *                 useRelinkIBKR(accountId) · useSyncInvestmentAccount(accountId) ·
 *                 usePortfolio(accountId, {enabled?}) · useInvestmentTrades(accountId,
 *                 {enabled?}) (the last 20)
 *   reports       useCategoryTotals({from?, to?, kind?: 'spend'|'income'}) ·
 *                 useFlow({from?, to?, granularity?: 'day'|'month'})
 *   transfers     useTransferSuggestions · useDecideTransfer
 *   rules         useRules · useSaveRule()({id?, input}) · useDeleteRule()(id)
 *   assistant     useProposals · useDecideProposal()({id, apply}) · useChatHistory ·
 *                 useClearChat
 *   PIN           useSecurityStatus · useSetPin · useRemovePin · useEnableDeviceForPin ·
 *                 useDisableDeviceForPin
 *   categories    useCategories (the tree: data.categories)
 *   any           useMoneyInvalidation(): refreshes every money key below
 */

export const keys = {
  me: ['me'] as const,
  accounts: ['accounts'] as const,
  account: (id: string) => ['account', id] as const,
  checkpoints: (id: string) => ['checkpoints', id] as const,
  reconciliation: (id: string) => ['reconciliation', id] as const,
  entries: (query: EntryQuery) => ['entries', query] as const,
  importRuns: (id: string) => ['import-runs', id] as const,
  chat: ['chat'] as const,
  investmentLink: (id: string) => ['investment-link', id] as const,
  portfolio: (id: string) => ['portfolio', id] as const,
  investmentTrades: (id: string) => ['investment-trades', id] as const,
  security: ['security'] as const,
  categories: ['categories'] as const,
  rules: ['rules'] as const,
  proposals: ['proposals'] as const,
  transfers: ['transfer-suggestions'] as const,
  captures: ['captures'] as const,
  captureReview: ['captures', 'review'] as const,
}

/**
 * Every query-key prefix that shows money or what it means: balances, movements,
 * the spending and flow reports, an account's reconciliation, the transfers waiting
 * for a yes or no, and the noted purchases waiting for review. Recategorizing a
 * movement shrinks "Sin categoría"; confirming a transfer takes it off the list; an
 * import changes all of them, and may settle or question a noted purchase.
 */
const MONEY_KEYS = [
  'me',
  'accounts',
  'account',
  'checkpoints',
  'entries',
  'import-runs',
  'portfolio',
  'reports',
  'reconciliation',
  keys.transfers[0],
  keys.captures[0],
] as const

/**
 * Invalidates everything that displays money. Every mutation that changes money
 * already calls it; use it directly only for a change made outside these hooks.
 */
export function useMoneyInvalidation() {
  const queryClient = useQueryClient()
  return () =>
    Promise.all(MONEY_KEYS.map((key) => queryClient.invalidateQueries({ queryKey: [key] })))
}

// --- accounts ---------------------------------------------------------------

export function useMe() {
  return useQuery({ queryKey: keys.me, queryFn: api.fetchMe })
}

export function useAccounts() {
  return useQuery({ queryKey: keys.accounts, queryFn: api.fetchAccounts })
}

export function useAccount(id: string) {
  return useQuery({ queryKey: keys.account(id), queryFn: () => api.fetchAccount(id) })
}

export function useUpdateAccount(id: string) {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (patch: { alias?: string; type?: 'checking' | 'savings' }) =>
      api.updateAccount(id, patch),
    onSuccess: invalidate,
  })
}

/**
 * Adds a card someone else pays. It starts with no purchases, so of everything that
 * shows money only the account list changes. The new account goes into the cache as
 * the answer brought it, so its page opens at once; the list's refresh is awaited, so
 * Cuentas already has it when the owner goes back.
 *
 * The form chooses the card's id, so sending it again after an answer that never
 * came gets the same card back (201), never a 409 about its own digits. Nothing is
 * refreshed on an error: a refresh could add the card to Cuentas and unmount the
 * dialog (opened from Cuentas' empty state) while it says what went wrong, and the
 * send again refreshes it anyway.
 */
export function useAddOthersCard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.addOthersCard,
    onSuccess: (account) => {
      queryClient.setQueryData(keys.account(account.id), account)
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.accounts }),
        queryClient.invalidateQueries({ queryKey: keys.me }),
      ])
    },
  })
}

/**
 * Deletes an account. The caller leaves the account's page in its own onSuccess
 * (`mutate(id, { onSuccess: () => navigate(…) })`), which runs right after this
 * one: the deleted account's own queries are dropped first, and the refresh of
 * everything else is not awaited. Awaiting it would refetch the account the page
 * is still showing, and its 404 would replace the page (and unmount the dialog)
 * before the caller's navigation and toast ever ran.
 */
export function useDeleteAccount() {
  const queryClient = useQueryClient()
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (id: string) => api.deleteAccount(id),
    onSuccess: (_data, id) => {
      for (const key of [
        keys.account(id),
        keys.checkpoints(id),
        keys.reconciliation(id),
        keys.importRuns(id),
        keys.investmentLink(id),
        keys.portfolio(id),
        keys.investmentTrades(id),
      ]) {
        queryClient.removeQueries({ queryKey: key, exact: true })
      }
      void invalidate()
    },
  })
}

export function useCheckpoints(accountId: string) {
  return useQuery({
    queryKey: keys.checkpoints(accountId),
    queryFn: () => api.fetchCheckpoints(accountId),
  })
}

export function useAddCheckpoint(accountId: string) {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (input: CheckpointInput) => api.addCheckpoint(accountId, input),
    onSuccess: invalidate,
  })
}

export function useDeleteCheckpoint(accountId: string) {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (checkpointId: string) => api.deleteCheckpoint(accountId, checkpointId),
    onSuccess: invalidate,
  })
}

export function usePinCheckpoint(accountId: string) {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (checkpointId: string | null) => api.pinCheckpoint(accountId, checkpointId),
    onSuccess: invalidate,
  })
}

export function useReconciliation(accountId: string, options: { enabled?: boolean } = {}) {
  return useQuery({
    queryKey: keys.reconciliation(accountId),
    queryFn: () => api.fetchReconciliation(accountId),
    enabled: options.enabled ?? true,
  })
}

// --- movements --------------------------------------------------------------

/** One page of movements; keeps the previous page visible while the next loads. */
export function useEntries(query: EntryQuery) {
  return useQuery({
    queryKey: keys.entries(query),
    queryFn: () => api.fetchEntries(query),
    placeholderData: (previous) => previous,
  })
}

/** Every page of movements matching a query, loaded on demand. */
export function useEntryPages(query: Omit<EntryQuery, 'cursor'>) {
  return useInfiniteQuery({
    queryKey: ['entries', 'pages', query],
    queryFn: ({ pageParam }) => api.fetchEntries({ ...query, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.has_more ? last.next_cursor : undefined),
  })
}

export function useUpdateEntry() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: EntryPatch }) =>
      api.updateEntry(id, patch),
    onSuccess: invalidate,
  })
}

// --- noted purchases ----------------------------------------------------------

/**
 * Notes a purchase by hand. A pending one shows in the lists, in the account's
 * "Pendiente" line and in Gastos; one its statement already printed changes how
 * much is pending to review. Either way, money.
 *
 * Sending it again is not safe: the server makes a new purchase each time. So a
 * failure refreshes money too, unless the server said no (a 4xx, which saved
 * nothing). With no answer, or a 5xx, the first one may have been saved, and the
 * account should show it before the owner notes it again.
 */
export function useAddCapture() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (input: CaptureInput) => api.addCapture(input),
    onSuccess: invalidate,
    // A send with no answer may have been saved; the account shows it if so. Not
    // awaited: with the connection gone the refetches retry for seconds, and the
    // dialog should say what happened at once.
    onError: (error) => {
      if (!(error instanceof ApiError && error.status < 500)) void invalidate()
    },
  })
}

/** Corrects a noted purchase's amount, merchant or day; whether it counts derives again. */
export function useEditCapture() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: CapturePatch }) =>
      api.editCapture(id, patch),
    onSuccess: invalidate,
  })
}

/** Discards a noted purchase that never happened: it stops counting. */
export function useDiscardCapture() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (id: string) => api.discardCapture(id),
    onSuccess: invalidate,
  })
}

/** The noted purchases a statement does not confirm, with the lines that could be them. */
export function useCaptureReview() {
  return useQuery({ queryKey: keys.captureReview, queryFn: api.fetchCaptureReview })
}

/*
 * Linking, rejecting and unlinking each refresh money. Each can change what counts: a
 * purchase whose only possible line was rejected, or that was unlinked, counts again;
 * a line that gets the purchase's category moves in Gastos; one answer can settle
 * another question (a "No" to one of two lines links the other).
 *
 * On success the refresh is awaited, so the caller goes on with the lists already
 * saying what is true now and never asks the owner a question that was just settled.
 * A failure refreshes too, since a refusal mostly means the screen was out of date
 * (another tab linked it first) and a send with no answer may have gone through, but
 * without waiting: with the connection gone the refetches retry for seconds, and the
 * owner should hear what failed at once.
 */

/** Links a noted purchase to the line the owner says it is. */
export function useLinkCapture() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: ({ id, entryId }: { id: string; entryId: string }) =>
      api.linkCapture(id, entryId),
    onSuccess: invalidate,
    onError: () => void invalidate(),
  })
}

/** Says a line is not this noted purchase ("No"). */
export function useRejectCapture() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: ({ id, entryId }: { id: string; entryId: string }) =>
      api.rejectCaptureLine(id, entryId),
    onSuccess: invalidate,
    onError: () => void invalidate(),
  })
}

/** Undoes a link the owner says is wrong ("No es esta compra"). */
export function useUnlinkCapture() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: (id: string) => api.unlinkCapture(id),
    onSuccess: invalidate,
    onError: () => void invalidate(),
  })
}

// --- the phone's shortcut ---------------------------------------------------------

/**
 * A key changes what an account says about its shortcut (`account.shortcut`, in the
 * account and in the list Ajustes reads) and nothing about money. The refresh is
 * awaited, so the dialog goes on with the account already saying what is true now.
 */
function useShortcutInvalidation(accountId: string) {
  const queryClient = useQueryClient()
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.accounts }),
      queryClient.invalidateQueries({ queryKey: keys.account(accountId) }),
    ])
}

/**
 * Makes the account's shortcut key, turning off the one it had. The answer is the
 * key itself, the one time the app ever sees it, and it must not linger: the caller
 * moves it into its own state and calls reset() at once, and gcTime 0 drops the
 * finished mutation, with the key in it, from the mutation cache as soon as nothing
 * observes it (by default it would wait five minutes).
 */
export function useCreateShortcutKey(accountId: string) {
  const invalidate = useShortcutInvalidation(accountId)
  return useMutation({
    mutationFn: () => api.createShortcutKey(accountId),
    onSuccess: () => invalidate(),
    gcTime: 0,
  })
}

/** Turns the account's shortcut key off. */
export function useRevokeShortcutKey(accountId: string) {
  const invalidate = useShortcutInvalidation(accountId)
  return useMutation({
    mutationFn: () => api.revokeShortcutKey(accountId),
    onSuccess: () => invalidate(),
  })
}

// --- imports -----------------------------------------------------------------

export function useImportStatement() {
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: ({ file, accountId }: { file: File; accountId?: string }) =>
      api.importStatement(file, accountId),
    onSuccess: invalidate,
  })
}

/** Parses a statement without importing it, so nothing is invalidated. */
export function useDryRunStatement() {
  return useMutation({ mutationFn: (file: File) => api.dryRunStatement(file) })
}

export function useImportRuns(accountId: string) {
  return useQuery({
    queryKey: keys.importRuns(accountId),
    queryFn: () => api.fetchImportRuns(accountId),
  })
}

// --- investments -------------------------------------------------------------

export function useInvestmentLink(accountId: string, options: { enabled?: boolean } = {}) {
  return useQuery({
    queryKey: keys.investmentLink(accountId),
    queryFn: () => api.fetchInvestmentLink(accountId),
    enabled: options.enabled ?? true,
    // Absent is an ordinary answer here, not a failure worth retrying.
    retry: false,
  })
}

export function useLinkIBKR() {
  const invalidate = useMoneyInvalidation()
  return useMutation({ mutationFn: api.linkIBKR, onSuccess: invalidate })
}

export function useRelinkIBKR(accountId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { flex_query_id: string; flex_token: string }) =>
      api.relinkIBKR(accountId, input),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: keys.investmentLink(accountId) }),
  })
}

export function useSyncInvestmentAccount(accountId: string) {
  const queryClient = useQueryClient()
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: () => api.syncInvestmentAccount(accountId),
    // Settled rather than success: a failed sync is recorded on the link too,
    // and the page should show it without a reload.
    onSettled: () => {
      void invalidate()
      void queryClient.invalidateQueries({ queryKey: keys.investmentLink(accountId) })
      void queryClient.invalidateQueries({ queryKey: keys.investmentTrades(accountId) })
    },
  })
}

export function usePortfolio(accountId: string, options: { enabled?: boolean } = {}) {
  return useQuery({
    queryKey: keys.portfolio(accountId),
    queryFn: () => api.fetchPortfolio(accountId),
    enabled: options.enabled ?? true,
  })
}

export function useInvestmentTrades(
  accountId: string,
  options: { enabled?: boolean } = {},
) {
  return useQuery({
    queryKey: keys.investmentTrades(accountId),
    queryFn: () => api.fetchInvestmentTrades(accountId, 20),
    enabled: options.enabled ?? true,
  })
}

// --- reports -------------------------------------------------------------------

export function useCategoryTotals(query: {
  from?: string
  to?: string
  kind?: 'spend' | 'income'
}) {
  return useQuery({
    queryKey: ['reports', 'categories', query],
    queryFn: () => api.fetchCategoryTotals(query),
  })
}

export function useFlow(query: {
  from?: string
  to?: string
  granularity?: 'day' | 'month'
}) {
  return useQuery({
    queryKey: ['reports', 'flow', query],
    queryFn: () => api.fetchFlow(query),
  })
}

// --- transfers -----------------------------------------------------------------

export function useTransferSuggestions() {
  return useQuery({ queryKey: keys.transfers, queryFn: api.fetchTransferSuggestions })
}

/**
 * Confirms or rejects a suggested transfer. Either way the suggestion leaves the
 * list, and a confirmed one changes spending, so both refresh.
 */
export function useDecideTransfer() {
  const queryClient = useQueryClient()
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: api.decideTransfer,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.transfers })
      void invalidate()
    },
  })
}

// --- rules ---------------------------------------------------------------------

export function useRules() {
  return useQuery({ queryKey: keys.rules, queryFn: api.fetchRules })
}

export function useSaveRule() {
  const queryClient = useQueryClient()
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: ({ id, input }: { id?: string; input: RuleInput }) =>
      id ? api.updateRule(id, input) : api.createRule(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.rules })
      void invalidate()
    },
  })
}

export function useDeleteRule() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.deleteRule(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.rules }),
  })
}

// --- proposals -----------------------------------------------------------------

export function useProposals() {
  return useQuery({ queryKey: keys.proposals, queryFn: api.fetchProposals })
}

/** Applies or discards a proposal. Applying can change categories, rules and checks. */
export function useDecideProposal() {
  const queryClient = useQueryClient()
  const invalidate = useMoneyInvalidation()
  return useMutation({
    mutationFn: ({ id, apply }: { id: string; apply: boolean }) =>
      apply ? api.applyProposal(id) : api.rejectProposal(id),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: keys.proposals })
      void queryClient.invalidateQueries({ queryKey: keys.rules })
      void invalidate()
    },
  })
}

// --- chat ---------------------------------------------------------------------

export function useChatHistory() {
  return useQuery({ queryKey: keys.chat, queryFn: api.fetchChatHistory })
}

export function useClearChat() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.clearChat,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.chat }),
  })
}

// --- security (PIN) ----------------------------------------------------

export function useSecurityStatus() {
  return useQuery({ queryKey: keys.security, queryFn: api.fetchSecurityStatus })
}

function useSecurityInvalidation() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: keys.security })
}

export function useSetPin() {
  const invalidate = useSecurityInvalidation()
  return useMutation({
    mutationFn: ({ currentPassword, pin }: { currentPassword: string; pin: string }) =>
      api.setPin(currentPassword, pin),
    onSuccess: invalidate,
  })
}

export function useRemovePin() {
  const invalidate = useSecurityInvalidation()
  return useMutation({
    mutationFn: (currentPassword: string) => api.removePin(currentPassword),
    onSuccess: invalidate,
  })
}

export function useEnableDeviceForPin() {
  const invalidate = useSecurityInvalidation()
  return useMutation({
    mutationFn: api.enableDeviceForPin,
    onSuccess: invalidate,
  })
}

export function useDisableDeviceForPin() {
  const invalidate = useSecurityInvalidation()
  return useMutation({
    mutationFn: api.disableDeviceForPin,
    onSuccess: invalidate,
  })
}

// --- categories ---------------------------------------------------------------

export function useCategories() {
  return useQuery({ queryKey: keys.categories, queryFn: api.fetchCategories })
}
