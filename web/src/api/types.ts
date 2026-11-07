/**
 * The API's wire shapes, written by hand to match the Go DTOs.
 *
 * Hand-written rather than generated from a spec: with this many endpoints a
 * schema plus codegen would cost more to keep in step than the types do, and
 * these carry comments explaining what a field means — which generated types
 * never do.
 */

/** An amount, always integer cents plus the same value as text. */
export interface Amount {
  /** The authoritative value. Compute with this; never with `formatted`. */
  cents: number
  /** The same amount as a decimal string, e.g. "32354.53". */
  formatted: string
  currency: string
}

export type AccountClass = 'asset' | 'liability'
export type AccountType = 'checking' | 'savings' | 'credit_card' | 'brokerage'
/** 'manual': a card someone else pays, which the owner added by hand (`paid_by_other`). */
export type Institution = 'banco_general' | 'bac' | 'ibkr' | 'manual'

/**
 * A real-world account: a bank account, a card or a brokerage account, keyed the
 * way its institution keys it.
 */
export interface Account {
  id: string
  class: AccountClass
  type: AccountType
  institution: Institution
  /** The number as the bank prints it, e.g. "04-98-97-958835-1" or "**** 1111". */
  external_number: string
  display_name: string
  /** What the owner calls it, when they have named it. Read through `accountLabel`. */
  alias?: string
  currency: string
  /**
   * Signed from the owner's point of view: a card that owes $14.30 has -14.30.
   * Only meaningful when `anchored`.
   */
  balance: Amount
  /**
   * A card's balance as the bank prints it: a positive amount owed. Absent for a card
   * someone else pays, whose debt is not the owner's.
   */
  owed?: Amount
  /**
   * A card someone else pays: the owner added it by hand and uses it, someone else
   * gets its statements and pays it. Its purchases count in the owner's spending,
   * but not in what they owe nor in their net worth. It never has movements of its
   * own, a statement or a starting balance, so it never lacks one or fails to match:
   * its `balance` is always zero and `anchored` false. Absent for every other account.
   */
  paid_by_other?: boolean
  /**
   * What a card someone else pays was used for this calendar month, positive: what it
   * shows in place of what is owed. Present, zero included, only for such a card.
   */
  this_month?: Amount
  /**
   * Whether a stated balance — a statement's opening, IBKR's cash report, or one
   * the owner entered — fixes where the account started. Without one the balance
   * is only the sum of the imported movements.
   */
  anchored: boolean
  /** A brokerage account's positions, at market value. */
  holdings?: Amount
  /** The latest stated balance against the computed one. */
  drift?: Drift
  movements: number
  last_day: string | null
  /**
   * A card's latest statement: from the newest checkpoint that has a due date.
   * Optional: older APIs do not send it, and every screen must render without it.
   */
  statement?: CardStatement
  /**
   * When a brokerage account last synced with IBKR. Optional for the same
   * reason; absent or null means unknown or never.
   */
  last_synced_at?: string | null
  /**
   * What the purchases the owner noted, and no statement has printed yet, come to.
   * Signed like the balance (negative) and never in it. Absent when there are none.
   */
  pending?: PendingPurchases
  /**
   * The balance with the pending purchases in it. Only for an anchored account that
   * has some: an unanchored balance is not the bank's to begin with.
   */
  estimated_balance?: Amount
  /** How many noted purchases the owner should look at; absent when none. */
  to_review?: number
  /** The account's shortcut key as the owner may know it; absent for a brokerage account. */
  shortcut?: ShortcutStatus
}

/** An account's noted purchases that count as spending until their line arrives. */
export interface PendingPurchases {
  /** Negative, like the balance. */
  amount: Amount
  count: number
}

/** Whether an account has a working shortcut key, and what can be shown of it: never the key. */
export interface ShortcutStatus {
  configured: boolean
  /** The key's last four characters. */
  hint?: string
  created_at?: string
  /** The last run by hand, which tests the shortcut and notes nothing. */
  tested_at?: string
  /** The last purchase the key noted. */
  last_capture_at?: string
  /** The first card name the shortcut sent. */
  card_label?: string
}

/**
 * A shortcut key just made (POST /api/captures/keys/{accountID}): the one answer that
 * ever carries the key itself. Show it once and keep it nowhere; the server only
 * keeps its hash, so it cannot be shown again.
 */
export interface NewShortcutKey {
  /** "chn_…": what the shortcut sends as its Authorization. */
  key: string
  /** Its last four characters, all the owner sees of it afterwards. */
  hint: string
  created_at: string
}

/** What a card's statement says is due. Dates are civil (YYYY-MM-DD). */
export interface CardStatement {
  closing_on: string
  due_on: string | null
  minimum_payment: Amount | null
}

/** How far a stated balance is from what the movements add up to. */
export interface Drift {
  as_of: string
  stated: Amount
  computed: Amount
  /** stated − computed. Zero means the account matches the bank. */
  difference: Amount
  source: 'statement_opening' | 'statement_closing' | 'broker' | 'manual'
}

/** Assets minus debts plus investments. */
export interface NetWorth {
  assets: Amount
  owed: Amount
  holdings: Amount
  total: Amount
  /** True while some account with movements has no starting balance. */
  incomplete: boolean
  unanchored: number
}

export interface AccountList {
  accounts: Account[]
  net_worth: NetWorth
}

/** A balance somebody stated. */
export interface Checkpoint {
  id: string
  as_of: string
  balance: Amount
  source: Drift['source']
  /** The anchor the opening balance is derived from. */
  pinned: boolean
  due_on?: string
  minimum_payment?: Amount
  note?: string
}

export interface User {
  id: string
  email: string
  full_name: string
  created_at: string
}

export interface Session {
  access_token: string
  token_type: string
  /** When the access token expires, so the client refreshes before it fails. */
  expires_at: string
  user: User
}

export interface Me {
  user: User
  accounts: Account[]
  net_worth: NetWorth
}

/** Whether this browser can skip straight to a PIN, and whose it is. */
export interface DeviceStatus {
  trusted: boolean
  full_name?: string
}

/** What the Seguridad screen renders its PIN section from. */
export interface SecurityStatus {
  has_pin: boolean
  device_enabled: boolean
}

/**
 * What a movement counts as. A transfer between the owner's own accounts and a
 * trade never count as spending or income.
 */
export type EntryKind =
  'income' | 'expense' | 'refund' | 'fee' | 'interest' | 'transfer' | 'trade'

/**
 * One line a statement or IBKR printed, or a purchase the owner noted that no
 * statement has printed yet (`origin: 'capture'`).
 */
export interface Entry {
  id: string
  account_id: string
  /** The day the bank printed (YYYY-MM-DD), with no time zone to shift it. */
  booked_on: string
  posted_on?: string
  /** For a purchase the shortcut noted, the moment it arrived. */
  booked_at?: string
  /** Signed from the owner's point of view: negative is money leaving. */
  amount: Amount
  kind: EntryKind
  /** What the importer decided, when the owner or a rule overrode it. */
  imported_kind: EntryKind
  description: string
  bank_ref?: string
  bank_category?: string
  /** The balance the bank printed after this line. */
  bank_balance?: Amount
  category_id?: string
  category_source?: 'rule' | 'user' | 'assistant'
  note?: string
  /**
   * 'statement' for a printed line; 'capture' for a purchase the owner noted that no
   * statement has printed yet, which counts as spending but is not in the balance.
   * Optional: older APIs do not send it, and absent means a printed line.
   */
  origin?: 'statement' | 'capture'
  /**
   * How a capture was noted first: by the phone's shortcut, by hand, or by the
   * assistant from what it read.
   */
  capture_source?: CaptureSource
  /**
   * On a printed line from the movement lists, the purchase the owner noted that was
   * linked to it. Absent on any other movement, and from older APIs.
   */
  noted?: NotedPurchase
  /**
   * The text a rule made from this movement would match: the shop its description
   * names, without card, city or order number, as printed. Absent from older APIs.
   */
  rule_hint?: string
  /**
   * The rule text names a person paid or paying by phone rather than a shop: a rule
   * for them only fits a fixed payment, so it is never offered on its own.
   */
  rule_hint_person?: boolean
}

/** What a rule's text would reach before it exists. */
export interface RuleReach {
  /** Movements it matches: lines, and noted purchases that still count. */
  matches: number
  /** Of them, spending that has no category yet. */
  uncategorized: number
}

/** The way a noted purchase reached chenchén first. */
export type CaptureSource = 'shortcut' | 'manual' | 'agent'

/** The noted purchase a printed line turned out to be. */
export interface NotedPurchase {
  capture_id: string
  /** What the owner called it: "Café Unido". */
  merchant: string
  source: CaptureSource
  /** Whether chenchén linked them on its own or the owner did. */
  linked_by: 'auto' | 'user'
}

/**
 * Where a noted purchase stands, derived by the server whenever it is read. Only
 * `pending` counts as spending: `printed` (a line of the same amount covers it),
 * `linked` (joined to its line) and `missing` (a statement passed its day without
 * it) leave the counting to the statement, and `discarded` never happened.
 */
export type CaptureState = 'pending' | 'printed' | 'missing' | 'linked' | 'discarded'

/** A purchase the owner noted, as /api/captures returns it: the movement it reads as. */
export interface Capture extends Entry {
  state: CaptureState
  linked_by?: 'auto' | 'user'
  /** Once linked, the printed line it turned out to be. */
  line?: Entry
  /**
   * On the answer to noting a purchase (a 200, not a 201): another way, the shortcut
   * say, had noted it already, so nothing new counts. It is that purchase, with its
   * own amount, day and merchant; what was typed only filled in a note or a category
   * it lacked. Absent otherwise.
   */
  already_noted?: boolean
}

/**
 * A noted purchase the owner should look at (GET /api/captures/review). Neither state
 * counts as spending, so answering is about getting the record right, not the totals.
 *   printed  lines of the same amount could be it and the choice was not obvious:
 *            `candidates`, oldest first.
 *   missing  a statement went past its day without it: `near` holds the lines that
 *            come close (a tip, a late charge), best first, at most five.
 * Both lists are always there, empty when there is nothing in them.
 */
export interface CaptureReviewItem extends Capture {
  state: 'printed' | 'missing'
  candidates: Entry[]
  near: Entry[]
}

export interface EntryPage {
  entries: Entry[]
  /** Opaque; pass it back verbatim to get the next page. */
  next_cursor?: string
  has_more: boolean
}

/**
 * Why the assistant is not answering, when it is not.
 *
 * `is_ai` alone cannot tell these apart, and they are not the same message. Running
 * without a key is a configuration choice; running out of budget means the money
 * for the model is gone. One of those is worth explaining.
 */
export type ChatEngine = 'ai' | 'unconfigured' | 'budget_exhausted' | 'degraded'

export interface ChatProvider {
  name: string
  /** False when the assistant is unavailable, which the interface says plainly. */
  is_ai: boolean
  engine: ChatEngine
}

export interface ChatMessage {
  id: number
  role: 'user' | 'assistant' | 'tool'
  text?: string
  /** Names of the tools this turn ran, for the trace shown under the message. */
  tools?: string[]
  created_at: string
}

export interface ChatHistory {
  messages: ChatMessage[]
  provider: ChatProvider
}

/** The single error shape every endpoint returns. */
export interface ApiErrorBody {
  error: {
    code: string
    message: string
    /** Per-field messages, keyed by the JSON field name. */
    fields?: Record<string, string>
    request_id?: string
  }
}

// --- investments -------------------------------------------------------------

/** A brokerage account's IBKR link. Never includes the token. */
export interface InvestmentLinkStatus {
  ibkr_account_id: string
  last_synced_at?: string
  last_sync_status: 'never' | 'ok' | 'error'
  last_sync_error?: string
}

/** What one sync saw and did. */
export interface InvestmentSyncResult {
  run_id: string
  cash_new: number
  cash_duplicate: number
  /** Rows in a currency other than USD, left out and counted. */
  cash_failed: number
  trades_new: number
  trades_duplicate: number
  positions: number
  /** The period IBKR's report covers (YYYY-MM-DD). */
  period_from: string | null
  period_to: string | null
  generated_at?: string
  /** IBKR's own ending cash, which the computed cash must agree with. */
  reported_cash?: Amount
  missing_sections: string[]
  /** Spanish, ready to show. */
  warnings: string[]
}

/** One holding, as of the last sync. */
export interface InvestmentPosition {
  symbol: string
  asset_class: string
  quantity: number
  mark_price?: Amount
  market_value?: Amount
  cost_basis?: Amount
  as_of: string
}

export interface Portfolio {
  holdings: Amount
  positions: InvestmentPosition[]
}

export interface InvestmentTrade {
  id: string
  symbol: string
  asset_class: string
  side: 'buy' | 'sell'
  quantity: number
  price: Amount
  commission: Amount
  net_cash: Amount
  trade_date: string
}

// --- imports -------------------------------------------------------------------

/**
 * The format of a statement file. bg_card_statement is a card's closed statement as a
 * PDF: no lines and no opening, only the balance at its cut.
 */
export type StatementSource = 'bg_account' | 'bac_account' | 'bg_card' | 'bg_card_statement'

/** What one statement in an uploaded file did to its account. */
export interface ImportAccountResult {
  account_id: string
  display_name: string
  /** The file introduced this account. */
  created: boolean
  period_start: string
  period_end: string
  lines: number
  new: number
  duplicates: number
  opening?: Amount
  closing?: Amount
  chain_break?: { line_no: number; bank: Amount; computed: Amount }
  warnings: string[]
  /** A card whose file prints no balance, with no starting balance yet. */
  needs_opening: boolean
  /** Purchases the owner noted that this file linked to their lines; absent when none. */
  captures_linked?: number
  /**
   * Noted purchases this is the first statement to pass without printing; absent
   * when none.
   */
  captures_missing?: number
}

export interface ImportResult {
  source: StatementSource
  /** The exact same file was already imported; nothing changed. */
  unchanged: boolean
  accounts: ImportAccountResult[]
}

/** One import or IBKR sync, as recorded. */
export interface ImportRun {
  id: string
  source: StatementSource | 'ibkr_flex'
  status: 'ok' | 'unchanged' | 'error'
  filename?: string
  period_start: string | null
  period_end: string | null
  lines_seen: number
  lines_new: number
  lines_duplicate: number
  lines_failed: number
  details: { warnings?: string[]; missing_sections?: string[] }
  error?: string
  created_at: string
}

/**
 * Where a statement Claude sent stands. `needs_review` waits for the owner with its
 * file: it did not continue an account cleanly enough to enter on its own.
 */
export type StatementUploadStatus =
  | 'pending'
  | 'imported'
  | 'unchanged'
  | 'needs_review'
  | 'unreadable'
  | 'dismissed'
  | 'expired'

/** A statement Claude sent (GET /api/imports/uploads), oldest first. */
export interface StatementUpload {
  id: string
  filename: string
  status: StatementUploadStatus
  /** Why it did not enter on its own, in Spanish, ready to show. */
  reason?: string
  created_at: string
  /** The account it is for, when chenchén already has it; never the bank's name. */
  account?: { id: string; last4?: string; name: string }
}

// --- Claude ----------------------------------------------------------------------

/**
 * A connection Claude asked for, as the consent page shows it (GET
 * /api/oauth/requests/:id). Opening it is what lets the owner answer it.
 */
export interface ConnectRequest {
  /** "Claude" or "Claude Code". */
  client_name: string
  /** Where the answer goes: "claude.ai", or "localhost" for Claude Code. */
  redirect_host: string
  /** What the client asked for; the owner decides on writing either way. */
  scopes: string[]
  expires_at: string
}

/** Where the consent page sends the browser after the owner answers. */
export interface ConnectAnswer {
  redirect_to: string
}

/**
 * The MCP endpoint's protected-resource metadata (GET
 * /.well-known/oauth-protected-resource/api/mcp), as far as the app reads it.
 */
export interface McpResource {
  /** Where Claude reaches chenchén's tools: the API's PUBLIC_URL and /api/mcp. */
  resource?: string
}

/** One of the owner's live connections with Claude (GET /api/oauth/grants). */
export interface ClaudeConnection {
  id: string
  client_name: string
  /** 'read', and 'write' when the owner let it note and file too. */
  scopes: string[]
  created_at: string
  /** Null until Claude first uses it. */
  last_used_at: string | null
}

// --- categories ------------------------------------------------------------------

export interface Category {
  id: string
  parent_id?: string
  name: string
  kind: 'income' | 'expense'
  color?: string
  icon?: string
  is_system: boolean
}

export interface CategoryNode extends Category {
  children: Category[]
}

/** One server-sent event from the chat stream. */
export type ChatEvent =
  | { kind: 'message'; text: string }
  | { kind: 'tool_call'; tool: string }
  | { kind: 'tool_result'; tool: string; failed?: boolean }
  | { kind: 'error'; code: string; message: string }
  // A change the assistant proposed; nothing is changed until the owner applies it.
  | {
      kind: 'proposal'
      tool: string
      proposal: { proposal_id: string; kind: string; summary: string }
    }
  // The closing event carries which engine actually answered. It can differ from
  // what the page was told on load — a spend ceiling reached mid-session, a key
  // that stopped working — and a label that only refreshes with the page would
  // credit this reply to a model that did not write it.
  | { kind: 'done'; provider?: ChatProvider }

/** What a dry run read from one statement, written nowhere. */
export interface StatementReport {
  institution: string
  external_number: string
  display_name: string
  class: 'asset' | 'liability'
  type: string
  currency: string
  /** Civil dates (YYYY-MM-DD). */
  period_start: string | null
  period_end: string | null
  /**
   * The bank's own balances; absent when the format prints none (a card's exports).
   * A card's closed statement brings the closing alone, its balance at the cut.
   */
  opening?: Amount
  closing?: Amount
  available?: Amount
  held?: Amount
  line_count: number
  money_in: Amount
  money_out: Amount
  /** The first line whose printed balance disagrees with the computed one. */
  chain_break?: { line_no: number; bank: Amount; computed: Amount }
  warnings: string[]
}

export interface StatementFileReport {
  source: StatementSource
  statements: StatementReport[]
}

// --- reconciliation ------------------------------------------------------------

export interface ReconciliationCheck {
  checkpoint: Checkpoint
  computed: Amount
  /** stated − computed. */
  difference: Amount
}

export interface StatementCheck {
  id: string
  period_start: string
  period_end: string
  line_count: number
  opening?: ReconciliationCheck
  closing?: ReconciliationCheck
  /** This statement starts more than a day after the previous one ends. */
  gap: boolean
  warnings: string[]
}

export interface ReconciliationLine {
  id: string
  booked_on: string
  description: string
  amount: Amount
  /** The running balance the movements add up to. */
  computed: Amount
  /** The running balance the bank printed, when it prints one. */
  bank?: Amount
  /** bank − computed. */
  difference?: Amount
}

/** Where an account stops matching its bank, if it does. */
export interface Reconciliation {
  account: Account
  anchor?: Checkpoint
  opening: Amount
  checks: ReconciliationCheck[]
  statements: StatementCheck[]
  lines: ReconciliationLine[]
  /** The line where the difference with the bank starts. */
  first_break?: string
}

// --- reports, transfers, rules ---------------------------------------------------

export interface CategoryTotal {
  /** Null for movements with no category. */
  category_id: string | null
  /** Positive: spending as money spent, a refund reducing it. */
  total: Amount
  count: number
}

export interface FlowPoint {
  /** The day, or the first of the month for a monthly series. */
  day: string
  in: Amount
  out: Amount
}

/** Two movements that look like one transfer between the owner's accounts. */
export interface TransferSuggestion {
  out: Entry
  in: Entry
}

export interface CategoryRule {
  id: string
  match_text: string
  category_id?: string
  /** Matching movements count as transfers between the owner's accounts. */
  transfer: boolean
  priority: number
  applied?: number
}

// --- proposals -------------------------------------------------------------------

/** A change the assistant proposed, waiting for the owner to apply or discard it. */
export interface Proposal {
  id: string
  kind: 'recategorize' | 'note' | 'rule' | 'transfer_decision' | 'checkpoint'
  /** Written by the server from the change itself, never by the model. */
  summary: string
  status: 'pending' | 'applied' | 'rejected'
  expires_at: string
}

// --- the month's plan --------------------------------------------------------------

/**
 * Where a month stands against today in Panama, which the server decides: the
 * browser's clock is never the authority on which month has passed.
 */
export type PlanState = 'current' | 'past' | 'future'

/**
 * How much of what is free went out. `no_free` is the plan's own shape, not a
 * warning: what goes out and what is set aside come to the income or more.
 */
export type PlanLevel = 'ok' | 'near' | 'over' | 'no_free'

export type PlanItemKind = 'income' | 'fixed' | 'saving' | 'purchase'

/** Which months an edit holds for: the month and every one after it, or that month alone. */
export type PlanScope = 'from' | 'only'

/**
 * An item of the plan with the values that hold for the month read. Months are
 * YYYY-MM; `to` is null when it has no end.
 */
export interface PlanItem {
  id: string
  kind: PlanItemKind
  name: string
  amount: Amount
  /** The day of the month it is expected, 1 to 31. Absent for a saving with none. */
  day?: number
  /** An expense category for a payment, an income one for a paycheck. */
  category_id?: string
  /** The text its movements contain, compared like a rule's; '' for none. */
  match_text: string
  from: string
  to: string | null
  /** A saving's goal. */
  target?: Amount
  target_month?: string
  /** The savings or brokerage account a saving is kept in. */
  account_id?: string
}

/**
 * A movement the plan read: a printed line, or a noted purchase that still counts.
 * As an item's candidate or hint it is only offered, and says nothing more.
 */
export interface PlanMovement {
  id: string
  origin: 'statement' | 'capture'
  booked_on: string
  /** Signed from the owner's point of view, as in Entry. */
  amount: Amount
  description: string
}

/**
 * A movement that paid a payment or brought in a paycheck, and who said so: the plan
 * by itself, by its text, amount and day (`auto`), or the owner (`assigned`), by the
 * link `link_id`, which deleting takes back.
 */
export type PlanPaidMovement = PlanMovement &
  ({ how: 'auto' } | { how: 'assigned'; link_id: string })

/**
 * Whether the owner said a month of an item was paid, or came in, where chenchén
 * cannot see (an account it does not hold), and by which link: deleting it takes
 * that back. It counts once, as planned.
 */
export type PlanMarkedPaid =
  { marked_paid: false } | { marked_paid: true; paid_link_id: string }

/**
 * A paycheck's month: when it was due, whether it arrived and what it arrived as.
 * `candidates` are deposits it could be, closest first, at most three, while it has
 * not arrived.
 */
export type PlanIncomeLine = PlanItem &
  PlanMarkedPaid & {
    kind: 'income'
    due_on: string
    status: 'done' | 'upcoming' | 'late' | 'missed'
    movements: PlanPaidMovement[]
    candidates: PlanMovement[]
  }

/**
 * A fixed payment's month. `paid` is what went out for it, null until a movement
 * says. While it is still to be paid, `candidates` are the movements it could be,
 * closest first, at most three, and `hint` a movement with its text that went out as
 * a transfer between the owner's accounts: if it is not one, it is this payment.
 */
export type PlanFixedLine = PlanItem &
  PlanMarkedPaid & {
    kind: 'fixed'
    due_on: string
    status: 'out' | 'upcoming' | 'late' | 'missed'
    paid: Amount | null
    movements: PlanPaidMovement[]
    candidates: PlanMovement[]
    hint: { movement: PlanMovement; reason: 'transfer' } | null
  }

/** What a month sets aside. Progress and what reached the account come later: null. */
export type PlanSavingLine = PlanItem & {
  kind: 'saving'
  status: string | null
  saved_month: Amount | null
  progress: Amount | null
  needed_per_month: Amount | null
}

/** A cap on a category, and what its free spending, children included, took of it. */
export interface PlanCapLine {
  category_id: string
  amount: Amount
  from: string
  to: string | null
  spent: Amount
  level: 'ok' | 'near' | 'over'
}

/** Half a month: the 1st to the 15th, or the 16th to its end. */
export interface PlanQuincena {
  from: string
  to: string
  /** Its share of what is free, by its days. */
  free: Amount
  spent: Amount
  income_expected: Amount
  fixed_due: Amount
}

/**
 * A card's latest statement as the plan shows it: when it is due, and whether what
 * reached it since it closed covers the minimum (or, with none, that something did).
 * Information only: paying a card is a transfer, and its purchases already count as
 * spending.
 */
export interface PlanCardDue {
  account_id: string
  closing_on: string
  due_on: string
  minimum_payment: Amount | null
  paid: boolean
}

/**
 * What the current month has to say, which the server works out so the web and
 * Claude say the same: what is free, or a cap, at 80% or past it; a payment, or a
 * card, due within three days. Only the current month has any.
 */
export type PlanAlert =
  | { kind: 'free'; level: 'near' | 'over' }
  | { kind: 'cap'; level: 'near' | 'over'; category_id: string }
  | { kind: 'payment'; level: 'soon'; due_on: string; item_id: string }
  | { kind: 'payment'; level: 'soon'; due_on: string; account_id: string }

/** Which way a link goes: this movement paid it, this one did not, or it was paid outside. */
export type PlanLinkDecision = 'assign' | 'reject' | 'paid'

/**
 * What the owner said about one month of a paycheck or a payment. `movement_id` is
 * the movement as they named it, a line or a noted purchase; null for a month paid
 * outside.
 */
export interface PlanLink {
  id: string
  item_id: string
  month: string
  decision: PlanLinkDecision
  movement_id: string | null
  created_at: string
}

/** What the owner's history can suggest planning: a paycheck, a payment or a saving. */
export type PlanSuggestionKind = 'income' | 'fixed' | 'saving'

/**
 * Something the owner's history suggests planning, worked out each time it is asked
 * for (GET /api/plan/suggestions); nothing is planned until the owner says so.
 *
 * `key` names it, as a dismissal names it back: the text it was found by, with "#1"
 * or "#2" for each of two paychecks a month, or "account:<id>" for a saving. `name`
 * is chenchén's ("Yappy de cada mes" for a payment to a person, `person`: never the
 * person's name); `match_text` is the text as the statement prints it, what the plan
 * would recognise it by ('' for a saving). `amount` is the latest seen, or for a
 * saving what reached its account in a month on average, in whole dollars. `day` is
 * the usual day (absent for a saving), `months_seen` in how many months it came.
 */
export interface PlanSuggestion {
  key: string
  kind: PlanSuggestionKind
  name: string
  amount: Amount
  day?: number
  match_text: string
  /** The category its lines are filed under most often, if any. */
  category_id?: string
  months_seen: number
  last_on: string
  person: boolean
  /** A saving's account. */
  account_id?: string
}

/** GET /api/plan/suggestions: each list in the server's order, present even when empty. */
export interface PlanSuggestions {
  income: PlanSuggestion[]
  fixed: PlanSuggestion[]
  savings: PlanSuggestion[]
}

/**
 * GET /api/plan: a month's plan beside what happened in it.
 *
 *   free  = income − fixed − savings − purchases
 *   left  = free − spent
 *
 * `spent` is the month's spending that no planned item took, so Gastos' total is
 * `spent` plus what the payments took. Every list is present, empty when there is
 * nothing in it.
 */
export interface MonthPlan {
  month: string
  today: string
  state: PlanState
  /** The month has any item or cap. */
  planned: boolean
  /** The month can still be written: the current one or up to 24 after it. */
  editable: boolean
  summary: {
    income: Amount
    fixed: Amount
    savings: Amount
    purchases: Amount
    free: Amount
    spent: Amount
    left: Amount
    /** Today counts in the current month; all of a month ahead, none of a past one. */
    days_left: number
    /** `left` over `days_left`, rounded down; null once nothing is left. */
    per_day: Amount | null
    level: PlanLevel
  }
  income: {
    expected: Amount
    actual: Amount
    /** What came in that no paycheck of any month took. */
    unplanned: Amount
    items: PlanIncomeLine[]
  }
  fixed: PlanFixedLine[]
  savings: PlanSavingLine[]
  /** Empty until planned purchases are kept. */
  purchases: unknown[]
  caps: PlanCapLine[]
  uncategorized: { spent: Amount; count: number }
  quincenas: [PlanQuincena, PlanQuincena]
  /** The credit cards with a due date, beside the month: they never come off it. */
  cards: PlanCardDue[]
  alerts: PlanAlert[]
}
