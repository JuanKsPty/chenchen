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
