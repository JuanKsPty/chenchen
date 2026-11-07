package plan_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// Detect is pure, so what it finds in a history is checked here with made-up
// lines: the 6th of November 2026 is today, so July to October are the
// complete months it reads.

var (
	detectToday     = civil.Date{Year: 2026, Month: time.November, Day: 6}
	checkingAccount = uuid.New()
	cardAccount     = uuid.New()
)

func day(m time.Month, d int) civil.Date { return civil.Date{Year: 2026, Month: m, Day: d} }

// printed is a line a statement printed: spending is negative.
func printed(account uuid.UUID, on civil.Date, amount money.Cents, kind, text string) store.PlanMovement {
	return store.PlanMovement{ID: uuid.New(), Origin: store.OriginStatement, AccountID: account, BookedOn: on,
		Amount: amount, Kind: kind, Description: text}
}

// each is the same line on the day of each of months.
func each(account uuid.UUID, d int, amount money.Cents, kind, text string, months ...time.Month) []store.PlanMovement {
	var out []store.PlanMovement
	for _, m := range months {
		out = append(out, printed(account, day(m, d), amount, kind, text))
	}
	return out
}

func detect(lines ...[]store.PlanMovement) []plan.Suggestion {
	return plan.Detect(plan.DetectInput{Today: detectToday, Lines: slices.Concat(lines...)})
}

// suggested is the suggestion with key, or nil.
func suggested(found []plan.Suggestion, key string) *plan.Suggestion {
	for i := range found {
		if found[i].Key == key {
			return &found[i]
		}
	}
	return nil
}

func keys(found []plan.Suggestion) []string {
	out := make([]string, 0, len(found))
	for _, s := range found {
		out = append(out, s.Kind+" "+s.Key)
	}
	return out
}

func wantKeys(t *testing.T, found []plan.Suggestion, want ...string) {
	t.Helper()
	if got := keys(found); !slices.Equal(got, want) {
		t.Fatalf("suggested %q, want %q", got, want)
	}
}

func TestAMonthlyChargeSeenThreeMonthsIsSuggested(t *testing.T) {
	insurance := uuid.New()
	var lines []store.PlanMovement
	for i, at := range []civil.Date{day(time.August, 4), day(time.September, 3), day(time.October, 5)} {
		l := printed(checkingAccount, at, -263, store.KindFee, "PROTECCION ROBO 4000-00**-****-00000")
		if i > 0 {
			l.CategoryID = &insurance
		}
		lines = append(lines, l)
	}

	found := detect(lines)

	wantKeys(t, found, "fixed proteccion robo")
	s := found[0]
	if s.Name != "Proteccion Robo" || s.MatchText != "PROTECCION ROBO" || s.Amount != 263 || s.Day != 4 ||
		s.MonthsSeen != 3 || s.LastOn != day(time.October, 5) || s.Person || s.AccountID != nil {
		t.Errorf("suggestion = %+v", s)
	}
	if s.CategoryID == nil || *s.CategoryID != insurance {
		t.Errorf("category = %v, want the one its lines are filed under", s.CategoryID)
	}
}

func TestASemimonthlySalaryIsSuggestedAsTwoPaychecks(t *testing.T) {
	const salary = "ACH CRE EMPRESA X S.A."
	var lines []store.PlanMovement
	for _, d := range []struct {
		m             time.Month
		first, second int
	}{{time.July, 15, 30}, {time.August, 16, 29}, {time.September, 16, 30}, {time.October, 16, 30}} {
		lines = append(lines, printed(checkingAccount, day(d.m, d.first), 65000, store.KindIncome, salary),
			printed(checkingAccount, day(d.m, d.second), 65000, store.KindIncome, salary))
	}

	found := detect(lines)

	wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
	for i, wantDay := range []int{16, 30} {
		s := found[i]
		if s.Name != "Ach Cre Empresa X S.A." || s.MatchText != salary || s.Amount != 65000 || s.Day != wantDay ||
			s.MonthsSeen != 4 || s.Person {
			t.Errorf("paycheck %d = %+v", i+1, s)
		}
	}

	// On the 20th the first paycheck of November is in: it is the first
	// one's, by its day, and the latest the first one was seen.
	nov := printed(checkingAccount, day(time.November, 16), 66000, store.KindIncome, salary)
	found = plan.Detect(plan.DetectInput{Today: day(time.November, 20), Lines: append(lines, nov)})
	wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
	if first := found[0]; first.Amount != 66000 || first.LastOn != nov.BookedOn || first.MonthsSeen != 5 {
		t.Errorf("first paycheck on the 20th = %+v", first)
	}
}

func TestAPaycheckThatLandsInTheNextMonthIsStillTheSecond(t *testing.T) {
	const salary = "ACH CRE EMPRESA X S.A."
	var lines []store.PlanMovement
	for _, at := range []civil.Date{day(time.July, 16), day(time.July, 30), day(time.August, 14),
		day(time.August, 28), day(time.September, 16), day(time.September, 30), day(time.October, 16),
		// October's second, late.
		day(time.November, 2)} {
		lines = append(lines, printed(checkingAccount, at, 65000, store.KindIncome, salary))
	}
	in := plan.DetectInput{Today: detectToday, Lines: lines}

	found := plan.Detect(in)

	wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
	if first, second := found[0], found[1]; first.Day != 16 || first.LastOn != day(time.October, 16) ||
		second.Day != 30 || second.LastOn != day(time.November, 2) {
		t.Errorf("paychecks = %+v", found)
	}

	// The 30th's paycheck is planned, and that line paid it: the 16th's is
	// still to plan.
	item := plan.ActiveItem{ID: uuid.New(), Kind: plan.KindIncome, MatchText: salary, Day: 30}
	in.Active = []plan.ActiveItem{item}
	in.Recognised = map[uuid.UUID]uuid.UUID{lines[len(lines)-1].ID: item.ID}
	wantKeys(t, plan.Detect(in), "income ach cre empresa x s.a.#1")
}

// The owner's salary is paid twice a month: in the middle of it, on the 14th
// to the 16th, and at its end, on the 30th or the 31st, or late, on the 1st
// to the 3rd of the next month. A calendar month holds one, two, three or
// four of its lines.

const salaryText = "ACH CRE EMPRESA X S.A."

// salaryOn is the salary's line, $650.00, on each of days.
func salaryOn(days ...civil.Date) []store.PlanMovement {
	out := make([]store.PlanMovement, 0, len(days))
	for _, at := range days {
		out = append(out, printed(checkingAccount, at, 65000, store.KindIncome, salaryText))
	}
	return out
}

// wantPaycheck checks s is a paycheck of the salary: $650.00 on onDay, seen
// in months months, the latest on lastOn.
func wantPaycheck(t *testing.T, s plan.Suggestion, onDay, months int, lastOn civil.Date) {
	t.Helper()
	if s.Amount != 65000 || s.Day != onDay || s.MonthsSeen != months || s.LastOn != lastOn ||
		s.MatchText != salaryText || s.Name != "Ach Cre Empresa X S.A." {
		t.Errorf("paycheck %s = %+v, want $650.00 on the %d, seen in %d months, the latest on %s",
			s.Key, s, onDay, months, lastOn)
	}
}

func TestAPaycheckThatSlipsIntoTheNextMonthStaysTheMonthEndOne(t *testing.T) {
	// June's paycheck of the month's end came on the 1st of July, August's on
	// the 2nd of September: July and September hold three lines, August one.
	lines := salaryOn(day(time.July, 1), day(time.July, 15), day(time.July, 31), day(time.August, 14),
		day(time.September, 2), day(time.September, 15), day(time.September, 30), day(time.October, 16),
		day(time.October, 30))

	found := detect(lines)

	wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
	wantPaycheck(t, found[0], 15, 4, day(time.October, 16))
	// July's, August's on the 2nd, September's and October's: June's is of a
	// month before the ones read.
	wantPaycheck(t, found[1], 30, 4, day(time.October, 30))

	// A month on, November's came on the 2nd of December: it is still the
	// one at the month's end, now late as often as not, and its median is
	// the 31st, the month's last day, never the 2nd.
	lines = append(lines, salaryOn(day(time.November, 16), day(time.December, 2))...)
	found = plan.Detect(plan.DetectInput{Today: day(time.December, 4), Lines: lines})

	wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
	wantPaycheck(t, found[0], 15, 4, day(time.November, 16))
	wantPaycheck(t, found[1], 31, 4, day(time.December, 2))
}

func TestMonthsWithThreeOrFourPaychecksStillMakeTwo(t *testing.T) {
	lines := salaryOn(
		// July holds four: June's late one, its two, and something else the
		// same employer paid once.
		day(time.July, 2), day(time.July, 15), day(time.July, 30),
		// August holds only the 16th: the statement is missing August's
		// paycheck of the month's end.
		day(time.August, 16),
		// September holds only the 15th: its paycheck of the month's end slipped
		// to the 1st of October, which holds three.
		day(time.September, 15), day(time.October, 1), day(time.October, 16), day(time.October, 30))
	lines = append(lines, printed(checkingAccount, day(time.July, 15), 30000, store.KindIncome, salaryText))

	found := detect(lines)

	wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
	wantPaycheck(t, found[0], 15, 4, day(time.October, 16))
	// Seen in July, September and October: two of the last three months,
	// the last one among them.
	wantPaycheck(t, found[1], 30, 3, day(time.October, 30))
}

func TestAMidMonthPaycheckThatMovesADayKeepsItsDay(t *testing.T) {
	// The paycheck of the month's end came late in four months of five, so
	// July and August each hold it before the middle of the month's: by its
	// place in those months, the first paycheck would be the one of the 2nd.
	// The middle of the month's moves between the 14th and the 16th.
	lines := salaryOn(day(time.July, 2), day(time.July, 15), day(time.August, 3), day(time.August, 14),
		day(time.September, 1), day(time.September, 16), day(time.September, 30), day(time.October, 15),
		day(time.November, 2))
	in := plan.DetectInput{Today: detectToday, Lines: lines}

	found := plan.Detect(in)

	wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
	wantPaycheck(t, found[0], 15, 4, day(time.October, 15))
	// Of the months read, it came on the 30th once and past the month's end
	// three times: their median is past the end, so it is planned for the
	// last day.
	wantPaycheck(t, found[1], 31, 4, day(time.November, 2))

	// The middle of the month's paycheck, planned on the 15th and paid on the
	// 16th of November, takes the first; the other is still suggested.
	item := plan.ActiveItem{ID: uuid.New(), Kind: plan.KindIncome, MatchText: salaryText, Day: 15}
	nov := salaryOn(day(time.November, 16))
	in.Today, in.Lines = day(time.November, 20), append(lines, nov...)
	in.Active, in.Recognised = []plan.ActiveItem{item}, map[uuid.UUID]uuid.UUID{nov[0].ID: item.ID}
	found = plan.Detect(in)
	wantKeys(t, found, "income ach cre empresa x s.a.#2")
	wantPaycheck(t, found[0], 31, 4, day(time.November, 2))
}

func TestSomethingElseTheEmployerPaidOnAnotherDayIsLeftOut(t *testing.T) {
	// The months of TestMonthsWithThreeOrFourPaychecksStillMakeTwo, with what
	// else the employer paid once on a day neither paycheck comes on.
	paychecks := salaryOn(day(time.July, 2), day(time.July, 15), day(time.July, 30), day(time.August, 16),
		day(time.September, 15), day(time.October, 1), day(time.October, 16), day(time.October, 30))
	for _, on := range []civil.Date{day(time.July, 22), day(time.August, 8), day(time.August, 24)} {
		found := detect(paychecks, []store.PlanMovement{printed(checkingAccount, on, 8500, store.KindIncome, salaryText)})

		wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
		wantPaycheck(t, found[0], 15, 4, day(time.October, 16))
		wantPaycheck(t, found[1], 30, 3, day(time.October, 30))
	}
}

func TestWhatElseTheEmployerPaidOnPaydaysDoesNotSwapThePaychecks(t *testing.T) {
	// The paycheck of the month's end came late every month, and the employer
	// paid something else on the 15th of July and of September: half the
	// months hold two lines in the middle, so the paychecks are told apart by
	// their place in each month, and still the middle one is the first.
	lines := salaryOn(day(time.July, 2), day(time.July, 15), day(time.August, 3), day(time.August, 14),
		day(time.September, 1), day(time.September, 16), day(time.October, 2), day(time.October, 16),
		day(time.November, 2))
	lines = append(lines, printed(checkingAccount, day(time.July, 15), 20000, store.KindIncome, salaryText),
		printed(checkingAccount, day(time.September, 15), 20000, store.KindIncome, salaryText))

	found := detect(lines)

	first, second := suggested(found, "ach cre empresa x s.a.#1"), suggested(found, "ach cre empresa x s.a.#2")
	if first == nil || second == nil || first.Day != 15 || first.Amount != 65000 || second.Day > 3 {
		t.Errorf("paychecks = %+v, want the one in the middle of the month first", found)
	}
}

func TestAMonthEndPaycheckIsAwaitedInTheFirstDaysOfTheNextMonth(t *testing.T) {
	// The paycheck of the month's end has come as late as the 2nd; October's
	// comes on the 3rd of November.
	lines := salaryOn(day(time.July, 15), day(time.July, 31), day(time.August, 14), day(time.September, 2),
		day(time.September, 15), day(time.September, 30), day(time.October, 16))
	firstOut := []store.PlanDismissal{{Kind: plan.KindIncome, Key: "ach cre empresa x s.a.#1"}}

	// On the 1st and the 2nd, October's is on its way, not missing: both are
	// still suggested, each by its own key.
	for _, today := range []civil.Date{day(time.November, 1), day(time.November, 2)} {
		in := plan.DetectInput{Today: today, Lines: lines}
		found := plan.Detect(in)
		wantKeys(t, found, "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
		wantPaycheck(t, found[0], 15, 4, day(time.October, 16))
		wantPaycheck(t, found[1], 31, 3, day(time.September, 30))

		in.Dismissed = firstOut
		wantKeys(t, plan.Detect(in), "income ach cre empresa x s.a.#2")
	}

	// On the 3rd it came.
	late := append(slices.Clone(lines), salaryOn(day(time.November, 3))...)
	found := plan.Detect(plan.DetectInput{Today: day(time.November, 3), Lines: late, Dismissed: firstOut})
	wantKeys(t, found, "income ach cre empresa x s.a.#2")
	wantPaycheck(t, found[0], 31, 4, day(time.November, 3))

	// Had it not come by the 6th, later than it ever came, it would be
	// missing, and the paycheck that still comes is the employer's only one.
	wantKeys(t, plan.Detect(plan.DetectInput{Today: day(time.November, 6), Lines: lines}),
		"income ach cre empresa x s.a.")
}

func TestAPaymentMadeEarlySomeMonthsAndLateOthersIsStillOne(t *testing.T) {
	// Its days fall in two runs half a month apart, but never two in a month.
	found := detect([]store.PlanMovement{
		printed(checkingAccount, day(time.July, 4), -45000, store.KindExpense, "ALQUILER DE OFICINA"),
		printed(checkingAccount, day(time.August, 19), -45000, store.KindExpense, "ALQUILER DE OFICINA"),
		printed(checkingAccount, day(time.September, 20), -45000, store.KindExpense, "ALQUILER DE OFICINA"),
		printed(checkingAccount, day(time.October, 5), -45000, store.KindExpense, "ALQUILER DE OFICINA"),
	})

	wantKeys(t, found, "fixed alquiler de oficina")
	if s := found[0]; s.Amount != 45000 || s.MonthsSeen != 4 || s.LastOn != day(time.October, 5) {
		t.Errorf("suggestion = %+v", s)
	}
}

func TestAPaymentBookedEarlyAtTheEndOfTheMonthBeforeKeepsItsDay(t *testing.T) {
	const loan = "CUOTA PRESTAMO PERSONAL"
	for _, c := range []struct {
		name   string
		on     []civil.Date
		amount []money.Cents
		want   plan.Suggestion
	}{{
		name:   "a payment of the 4th booked once on the 31st",
		on:     []civil.Date{day(time.July, 4), day(time.August, 5), day(time.August, 31), day(time.October, 3), day(time.November, 4)},
		amount: []money.Cents{45000, 45000, 45000, 45000, 45000},
		want:   plan.Suggestion{Amount: 45000, Day: 4, MonthsSeen: 4, LastOn: day(time.November, 4)},
	}, {
		name:   "a payment due on the 1st, a holiday one year",
		on:     []civil.Date{day(time.July, 1), day(time.August, 1), day(time.August, 31), day(time.October, 1), day(time.November, 1)},
		amount: []money.Cents{45000, 45000, 45000, 45000, 45000},
		want:   plan.Suggestion{Amount: 45000, Day: 1, MonthsSeen: 4, LastOn: day(time.November, 1)},
	}, {
		// This month's line, at a new price, is this month's: it is not held
		// to October's amount, and the payment is still suggested.
		name:   "a payment of the 4th that went up this month",
		on:     []civil.Date{day(time.July, 31), day(time.September, 4), day(time.October, 5), day(time.November, 4)},
		amount: []money.Cents{45000, 45000, 45000, 56000},
		want:   plan.Suggestion{Amount: 45000, Day: 4, MonthsSeen: 3, LastOn: day(time.October, 5)},
	}} {
		var lines []store.PlanMovement
		for i, on := range c.on {
			lines = append(lines, printed(checkingAccount, on, -c.amount[i], store.KindExpense, loan))
		}

		found := detect(lines)

		wantKeys(t, found, "fixed cuota prestamo personal")
		if s := found[0]; s.Amount != c.want.Amount || s.Day != c.want.Day || s.MonthsSeen != c.want.MonthsSeen ||
			s.LastOn != c.want.LastOn {
			t.Errorf("%s: suggestion = %+v", c.name, s)
		}
	}
}

func TestTwoChargesWithTheSameTextOnTheSameDayKeepTheirAmounts(t *testing.T) {
	months := []time.Month{time.July, time.August, time.September, time.October}
	// Each run gives the lines new ids, as importing the statements again
	// would.
	for range 5 {
		found := detect(
			each(cardAccount, 9, -1599, store.KindExpense, "STREAMING UNO", months...),
			each(cardAccount, 9, -799, store.KindExpense, "STREAMING UNO", months...),
		)

		wantKeys(t, found, "fixed streaming uno#2", "fixed streaming uno#1")
		if found[0].Amount != 1599 || found[1].Amount != 799 {
			t.Fatalf("amounts = %s and %s, want each its own", found[0].Amount, found[1].Amount)
		}
	}
}

func TestATaxLineFoldsIntoTheChargeItCameWith(t *testing.T) {
	months := []time.Month{time.August, time.September, time.October}
	found := detect(
		each(cardAccount, 18, -350, store.KindFee, "SEGURO DE FRAUDE MENSUAL", months...),
		each(cardAccount, 18, -18, store.KindFee, "ITBMS CARGO POR SEGURO", months...),
		// Under a dollar, beside nothing: not worth planning.
		each(checkingAccount, 10, -50, store.KindFee, "CARGO POR SERVICIO", months...),
	)

	wantKeys(t, found, "fixed seguro de fraude mensual")
	if s := found[0]; s.Amount != 368 || s.Day != 18 || s.Name != "Seguro de Fraude Mensual" ||
		s.MatchText != "SEGURO DE FRAUDE MENSUAL" {
		t.Errorf("suggestion = %+v, want $3.68 on the 18th", s)
	}
}

func TestAPaymentToAPersonIsSuggestedWithoutTheirName(t *testing.T) {
	const yappy = "YAPPY BG A PERSONA UNO"
	found := detect(
		[]store.PlanMovement{
			printed(checkingAccount, day(time.August, 4), -3267, store.KindExpense, yappy),
			printed(checkingAccount, day(time.September, 6), -3267, store.KindExpense, yappy),
			// Something else paid to the same person that month.
			printed(checkingAccount, day(time.September, 21), -1500, store.KindExpense, yappy),
			printed(checkingAccount, day(time.October, 5), -3267, store.KindExpense, yappy),
			printed(checkingAccount, day(time.November, 5), -3267, store.KindExpense, yappy),
		},
	)

	wantKeys(t, found, "fixed persona uno")
	s := found[0]
	if s.Name != plan.PersonName || !s.Person || s.Amount != 3267 || s.Day != 5 || s.MonthsSeen != 4 {
		t.Errorf("suggestion = %+v", s)
	}
	if strings.Contains(strings.ToLower(s.Name), "persona") {
		t.Errorf("the name %q names the person", s.Name)
	}
	// The text the plan looks for is what the statement prints.
	if s.MatchText != "PERSONA UNO" {
		t.Errorf("match text = %q", s.MatchText)
	}
}

func TestAnotherPaymentEarlyThisMonthDoesNotHideTheUsualOne(t *testing.T) {
	const yappy = "YAPPY BG A PERSONA UNO"
	lines := append(each(checkingAccount, 5, -3267, store.KindExpense, yappy,
		time.July, time.August, time.September, time.October),
		// Something else paid to the same person, before this month's.
		printed(checkingAccount, day(time.November, 2), -1500, store.KindExpense, yappy))

	found := plan.Detect(plan.DetectInput{Today: day(time.November, 3), Lines: lines})

	wantKeys(t, found, "fixed persona uno")
	if s := found[0]; s.Amount != 3267 || s.MonthsSeen != 4 || s.LastOn != day(time.October, 5) {
		t.Errorf("suggestion = %+v", s)
	}
}

func TestADismissalHoldsAsTheMonthsRoll(t *testing.T) {
	const yappy = "YAPPY BG A PERSONA UNO"
	lines := append(each(checkingAccount, 5, -3267, store.KindExpense, yappy,
		time.August, time.September, time.October, time.November, time.December),
		printed(checkingAccount, civil.Date{Year: 2027, Month: time.January, Day: 5}, -3267, store.KindExpense, yappy),
		// Something else paid to the same person in two months: while both are
		// among the months read, the person is paid twice a month.
		printed(checkingAccount, day(time.August, 21), -1500, store.KindExpense, yappy),
		printed(checkingAccount, day(time.September, 25), -2000, store.KindExpense, yappy))

	for _, today := range []civil.Date{day(time.December, 6), {Year: 2027, Month: time.January, Day: 6}} {
		in := plan.DetectInput{Today: today, Lines: lines}
		wantKeys(t, plan.Detect(in), "fixed persona uno")
		in.Dismissed = []store.PlanDismissal{{Kind: plan.KindFixed, Key: "persona uno"}}
		wantKeys(t, plan.Detect(in))
	}
}

func TestWeeklyGroceriesAreNotAFixedPayment(t *testing.T) {
	var lines []store.PlanMovement
	for _, m := range []time.Month{time.July, time.August, time.September, time.October} {
		for i, d := range []int{3, 10, 17, 24} {
			lines = append(lines,
				printed(cardAccount, day(m, d), -money.Cents(3800+i*450), store.KindExpense, "SUPERMERCADO UNO"),
				// The same amount every week is still every week.
				printed(cardAccount, day(m, d), -1000, store.KindExpense, "LAVANDERIA DOS"))
		}
	}

	wantKeys(t, detect(lines))
}

func TestVaryingAmountsAreNotAFixedPayment(t *testing.T) {
	found := detect(
		[]store.PlanMovement{
			printed(checkingAccount, day(time.August, 12), -4000, store.KindExpense, "ELECTRICIDAD"),
			printed(checkingAccount, day(time.September, 12), -5500, store.KindExpense, "ELECTRICIDAD"),
			printed(checkingAccount, day(time.October, 12), -7100, store.KindExpense, "ELECTRICIDAD"),
			// Within a dollar of the usual: the same payment.
			printed(checkingAccount, day(time.August, 20), -1000, store.KindExpense, "AGUA POTABLE"),
			printed(checkingAccount, day(time.September, 20), -1080, store.KindExpense, "AGUA POTABLE"),
			printed(checkingAccount, day(time.October, 20), -1150, store.KindExpense, "AGUA POTABLE"),
		},
	)

	wantKeys(t, found, "fixed agua potable")
	if found[0].Amount != 1150 {
		t.Errorf("amount = %s, want the latest", found[0].Amount)
	}
}

func TestAChargeThatStoppedIsNotSuggested(t *testing.T) {
	found := detect(
		each(cardAccount, 2, -3000, store.KindExpense, "GIMNASIO CENTRAL", time.July, time.August, time.September),
		// Seen in only one of the last three complete months: not yet.
		each(cardAccount, 9, -1299, store.KindExpense, "STREAMING UNO", time.October, time.November),
		each(cardAccount, 9, -899, store.KindExpense, "MUSICA DOS", time.August, time.October),
	)

	wantKeys(t, found, "fixed musica dos")
}

func TestAOneOffPurchaseIsNeverSuggested(t *testing.T) {
	wantKeys(t, detect([]store.PlanMovement{
		printed(cardAccount, day(time.October, 12), -110200, store.KindExpense, "TIENDA DE CAMARAS"),
		printed(checkingAccount, day(time.July, 3), 20000, store.KindIncome, "DEPOSITO EN EFECTIVO"),
		printed(checkingAccount, day(time.September, 3), 15000, store.KindIncome, "DEPOSITO EN EFECTIVO"),
		printed(cardAccount, day(time.August, 8), -1230, store.KindExpense, "FARMACIA TRES"),
		printed(cardAccount, day(time.October, 8), -4500, store.KindExpense, "FARMACIA TRES"),
	}))
}

func TestOnlyPrintedSpendingAndIncomeAreGrouped(t *testing.T) {
	months := []time.Month{time.August, time.September, time.October}
	payment := each(checkingAccount, 10, -20000, store.KindExpense, "PAGO A TARJETA 4321", months...)
	inPair := map[uuid.UUID]bool{}
	for _, l := range payment {
		inPair[l.ID] = true
	}
	noted := each(cardAccount, 7, -1500, store.KindExpense, "CAFE UNO", months...)
	for i := range noted {
		noted[i].Origin = store.OriginCapture
	}
	found := plan.Detect(plan.DetectInput{Today: detectToday, InOpenPair: inPair, Lines: slices.Concat(
		payment, noted,
		each(checkingAccount, 1, -10000, store.KindTransfer, "TRANSFERENCIA A AHORROS", months...),
		each(cardAccount, 1, -5000, store.KindTrade, "COMPRA DE ACCIONES", months...),
		each(cardAccount, 3, 500, store.KindRefund, "DEVOLUCION TIENDA", months...),
		each(cardAccount, 4, 900, store.KindExpense, "REVERSO CARGO", months...),
	)})

	wantKeys(t, found)
}

func TestEachPlannedPaycheckTakesTheSuggestionNearestItsDay(t *testing.T) {
	const salary = "ACH CRE EMPRESA X S.A."
	var lines []store.PlanMovement
	for _, m := range []time.Month{time.August, time.September, time.October} {
		lines = append(lines, printed(checkingAccount, day(m, 16), 65000, store.KindIncome, salary),
			printed(checkingAccount, day(m, 30), 65000, store.KindIncome, salary))
	}
	paycheck := func(day int) plan.ActiveItem {
		return plan.ActiveItem{ID: uuid.New(), Kind: plan.KindIncome, MatchText: "ach cre empresa x", Day: day}
	}
	in := plan.DetectInput{Today: detectToday, Lines: lines}

	in.Active = []plan.ActiveItem{paycheck(28)}
	wantKeys(t, plan.Detect(in), "income ach cre empresa x s.a.#1")

	in.Active = []plan.ActiveItem{paycheck(15), paycheck(1)}
	wantKeys(t, plan.Detect(in))

	// A payment with the text is not a paycheck.
	in.Active = []plan.ActiveItem{{ID: uuid.New(), Kind: plan.KindFixed, MatchText: "EMPRESA X", Day: 16}}
	wantKeys(t, plan.Detect(in), "income ach cre empresa x s.a.#1", "income ach cre empresa x s.a.#2")
}

func TestSuggestionsComePaychecksFirstAndStopAtTwenty(t *testing.T) {
	months := []time.Month{time.August, time.September, time.October}
	var lines [][]store.PlanMovement
	for i := range 25 {
		text := "COMERCIO " + strings.Repeat("X", i+1)
		lines = append(lines, each(cardAccount, 1+i, -money.Cents(1000+i), store.KindExpense, text, months...))
	}
	lines = append(lines, each(checkingAccount, 20, 1000, store.KindIncome, "INTERESES GANADOS", months...))

	found := detect(lines...)

	if len(found) != 20 {
		t.Fatalf("%d suggestions, want 20", len(found))
	}
	if found[0].Kind != plan.KindIncome {
		t.Errorf("first = %+v, want the paycheck", found[0])
	}
	// The biggest payments come first: same months, more money.
	if found[1].Amount != 1024 || found[19].Amount != 1006 {
		t.Errorf("payments run from %s to %s", found[1].Amount, found[19].Amount)
	}
}
