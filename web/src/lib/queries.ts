import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'

import * as api from '@/api/endpoints'
import type { CheckpointInput, EntryPatch, EntryQuery, RuleInput } from '@/api/endpoints'

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
 *                 ({alias?, type?: 'checking'|'savings'}) · useDeleteAccount()(id)
 *   checkpoints   useCheckpoints(accountId) · useAddCheckpoint(accountId) ·
 *                 useDeleteCheckpoint(accountId)(checkpointId) ·
 *                 usePinCheckpoint(accountId)(checkpointId | null) ·
 *                 useReconciliation(accountId, {enabled?})
 *   movements     useEntries(query): one page · useEntryPages(query without cursor):
 *                 infinite, "Ver más" = fetchNextPage, a new query resets the pages ·
 *                 useUpdateEntry()({id, patch})
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
}

/**
 * Every query-key prefix that shows money or what it means: balances, movements,
 * the spending and flow reports, an account's reconciliation, and the transfers
 * waiting for a yes or no. Recategorizing a movement shrinks "Sin categoría";
 * confirming a transfer takes it off the list; an import changes all of them.
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
