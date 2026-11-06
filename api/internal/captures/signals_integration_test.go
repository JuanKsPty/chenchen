package captures_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

// A tap with Apple Pay reaches chenchén twice: the shortcut sends it, and the
// bank's email says the same a little later. Whichever comes first notes the
// purchase; the other only tells of it too.
func TestTheShortcutAndTheBanksEmailTellOfOnePurchase(t *testing.T) {
	r := newRig(t, 60, 12)
	r.allowEmail(t)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)

	// The shortcut first, the email a minute later.
	r.now = lateNight.Add(-2 * time.Hour)
	tapped := r.tap(t, key, "$12.00", "Farmacia")
	got, joined := r.email(t, card, 1200, r.now.Add(time.Minute))
	if !joined || got.ID != tapped {
		t.Errorf("the email = %s, joined %v; want it to join %s", got.ID, joined, tapped)
	}
	if n := r.count(t, card); n != 1 {
		t.Errorf("purchases = %d, want 1", n)
	}
	if channels := r.channels(t, tapped); fmt.Sprint(channels) != "[shortcut email]" {
		t.Errorf("signals = %v, want the shortcut's and the email's", channels)
	}

	// The email first, the shortcut later: the shortcut's reply is the one
	// it gets for any purchase, with the purchase the email noted.
	r.now = lateNight.Add(-time.Hour)
	emailed, joined := r.email(t, card, 3000, r.now.Add(-3*time.Minute))
	if joined || emailed.Source != store.ChannelEmail {
		t.Fatalf("the email = %+v, joined %v; want a purchase of its own", emailed, joined)
	}
	if tapped := r.tap(t, key, "$30.00", "Super 99"); tapped != emailed.ID {
		t.Errorf("the shortcut noted %s; want it to join %s", tapped, emailed.ID)
	}
	if channels := r.channels(t, emailed.ID); fmt.Sprint(channels) != "[email shortcut]" {
		t.Errorf("signals = %v, want the email's and the shortcut's", channels)
	}
	if n := r.count(t, card); n != 2 {
		t.Errorf("purchases = %d, want 2", n)
	}
	if spent := r.spent(t); spent != 4200 {
		t.Errorf("spent = %s, want 42.00: each purchase once", spent)
	}
	// Joining changed nothing about the purchase the email noted.
	if c := r.get(t, r.user, emailed.ID); c.Source != store.ChannelEmail || c.Merchant != emailed.Merchant ||
		c.KeyID != nil || !c.ReceivedAt.Equal(emailed.ReceivedAt) || !c.UpdatedAt.Equal(emailed.UpdatedAt) {
		t.Errorf("after the shortcut joined: %+v\nbefore: %+v", c, emailed)
	}
}

// The email's delay is the whole point: a quarter of an hour between the tap
// and its email, or midnight between them, is still one purchase.
func TestChannelsSixteenMinutesApartTellOfOnePurchase(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)

	r.now = lateNight.Add(-3 * time.Hour)
	tapped := r.tap(t, key, "$8.75", "Cine")
	if got, joined := r.email(t, card, 875, r.now.Add(16*time.Minute)); !joined || got.ID != tapped {
		t.Errorf("16 minutes later = %s, %v; want it to join %s", got.ID, joined, tapped)
	}

	// Tapped at 23:30, emailed at 00:20: two days in Panama, one purchase.
	r.now = lateNight
	late := r.tap(t, key, "$15.00", "Restaurante")
	if got, joined := r.email(t, card, 1500, lateNight.Add(50*time.Minute)); !joined || got.ID != late {
		t.Errorf("after midnight = %s, %v; want it to join %s", got.ID, joined, late)
	}
	if n := r.count(t, card); n != 2 {
		t.Errorf("purchases = %d, want 2", n)
	}
}

// What another channel tells of a purchase never changes it: one linked to
// its line, or discarded, stays so and never counts again. The bank's email
// joins either; typing joins the linked one only (see
// TestTypingAPurchaseTheOwnerDiscardedNotesANewOne).
func TestALinkedOrDiscardedPurchaseStaysSoWhenAnotherChannelTellsOfIt(t *testing.T) {
	r := newRig(t, 60, 12)
	ctx := context.Background()
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	line := r.line(t, card, day, -950, store.KindExpense, "KIOSCO 123")

	linked := r.tap(t, key, "$9.50", "Kiosco")
	if c := r.get(t, r.user, linked); c.State != store.CaptureLinked || *c.EntryID != line.ID {
		t.Fatalf("fixture: %+v; want linked to its line at once", c)
	}
	declined := r.tap(t, key, "$5.00", "Kiosco")
	if _, err := r.svc.Discard(ctx, r.user, declined); err != nil {
		t.Fatal(err)
	}
	before := r.spent(t)
	if before != 950 {
		t.Fatalf("fixture: spent = %s, want the line only", before)
	}

	for _, tt := range []struct {
		paid  money.Cents
		id    uuid.UUID
		state string
	}{{950, linked, store.CaptureLinked}, {500, declined, store.CaptureDiscarded}} {
		got, joined := r.email(t, card, tt.paid, r.now.Add(4*time.Minute))
		if !joined || got.ID != tt.id || got.State != tt.state {
			t.Errorf("the email of %s = %+v, joined %v; want it to join the %s purchase", tt.paid, got, joined, tt.state)
		}
		if c := r.get(t, r.user, tt.id); c.State != tt.state {
			t.Errorf("after the email: %s, want %s", c.State, tt.state)
		}
	}
	noted, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: 950, Merchant: "Kiosco", Day: day,
		Note: "chicles"})
	if err != nil || !noted.AlreadyNoted || noted.ID != linked || noted.State != store.CaptureLinked || noted.Note != "" {
		t.Errorf("typing 9.50 = %+v, %v; want the linked purchase, already noted and as it was", noted, err)
	}
	if n := r.count(t, card); n != 2 {
		t.Errorf("purchases = %d, want 2", n)
	}
	if spent := r.spent(t); spent != before {
		t.Errorf("spent = %s, want %s still", spent, before)
	}
	if l := r.lineByID(t, line.ID); l.Amount != -950 || l.Description != "KIOSCO 123" {
		t.Errorf("the line = %+v", l)
	}
}

// A channel never tells of one purchase twice: two of its signals are two
// purchases, even of the same amount on the same day.
func TestTwoSignalsOfOneChannelAreTwoPurchases(t *testing.T) {
	r := newRig(t, 60, 12)
	r.allowEmail(t)
	ctx := context.Background()
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)

	r.now = lateNight.Add(-4 * time.Hour)
	first := r.tap(t, key, "$3.50", "Café Unido")
	r.now = r.now.Add(5 * time.Minute)
	second := r.tap(t, key, "$3.50", "Café Unido")
	if first == second || r.count(t, card) != 2 {
		t.Fatalf("two taps = %s and %s, %d purchases; want two", first, second, r.count(t, card))
	}
	// Each email joins one, the closest; a third is a purchase of its own.
	if got, joined := r.email(t, card, 350, r.now.Add(time.Minute)); !joined || got.ID != second {
		t.Errorf("the first email = %s, %v; want the second tap, the closest", got.ID, joined)
	}
	if got, joined := r.email(t, card, 350, r.now.Add(2*time.Minute)); !joined || got.ID != first {
		t.Errorf("the second email = %s, %v; want the first tap", got.ID, joined)
	}
	if _, joined := r.email(t, card, 350, r.now.Add(3*time.Minute)); joined {
		t.Error("a third email joined a purchase that already has one")
	}
	if n := r.count(t, card); n != 3 {
		t.Errorf("purchases = %d, want 3", n)
	}

	// Two typed by hand on one day are two as well.
	checking := r.account(t, r.user, "checking")
	for i := range 2 {
		c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: checking.ID, Amount: 1000, Merchant: "Taxi", Day: day})
		if err != nil || c.AlreadyNoted {
			t.Errorf("typed purchase %d = %+v, %v; want a new one", i+1, c, err)
		}
	}
	if n := r.count(t, checking); n != 2 {
		t.Errorf("typed purchases = %d, want 2", n)
	}
	if spent := r.spent(t); spent != 3*350+2*1000 {
		t.Errorf("spent = %s, want 30.50", spent)
	}
}

// Typing a purchase the shortcut already sent says so, and returns that one:
// nothing new counts, and its amount, day and merchant are the shortcut's.
// The note typed with it is kept, since it had none
// (TestWhatWasTypedFillsInWhatThePurchaseLacks).
func TestTypingAPurchaseAlreadyNotedSaysSo(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	tapped := r.tap(t, key, "$25.00", "Gasolinera")
	r.now = lateNight.Add(72 * time.Hour) // so the days after are not ahead of today
	jwt := r.bearer(t, r.user)
	typed := func(on civil.Date) string {
		return fmt.Sprintf(`{"account_id": %q, "amount": "25.00", "merchant": "Gasolina", "day": %q, "note": "lleno"}`,
			card.ID, on)
	}

	// The day after, typed the next morning: the same purchase.
	rec := r.serve(t, jwt, http.MethodPost, "/api/captures", typed(day.AddDays(1)))
	var v struct {
		ID           uuid.UUID `json:"id"`
		Description  string    `json:"description"`
		Note         string    `json:"note"`
		BookedOn     string    `json:"booked_on"`
		State        string    `json:"state"`
		AlreadyNoted *bool     `json:"already_noted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != http.StatusOK || v.AlreadyNoted == nil ||
		!*v.AlreadyNoted || v.ID != tapped || v.Description != "Gasolinera" || v.Note != "lleno" ||
		v.BookedOn != day.String() || v.State != store.CapturePending {
		t.Errorf("POST the day after = %d %s; want the shortcut's purchase, already noted", rec.Code, rec.Body)
	}
	if n := r.count(t, card); n != 1 {
		t.Errorf("purchases = %d, want 1", n)
	}
	if spent := r.spent(t); spent != 2500 {
		t.Errorf("spent = %s, want 25.00 once", spent)
	}

	// Two days after is another purchase, and a new one never says
	// already_noted at all.
	rec = r.serve(t, jwt, http.MethodPost, "/api/captures", typed(day.AddDays(2)))
	var fresh map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fresh); err != nil || rec.Code != http.StatusCreated || fresh["id"] == tapped.String() {
		t.Errorf("POST two days after = %d %s; want a new purchase", rec.Code, rec.Body)
	}
	if _, said := fresh["already_noted"]; said {
		t.Errorf("a new purchase says already_noted: %s", rec.Body)
	}
	if n := r.count(t, card); n != 2 {
		t.Errorf("purchases = %d, want 2", n)
	}
}

// The assistant notes what it read through the same door as the owner's
// hand, as a channel of its own with no time of day.
func TestTheAssistantNotesThroughTheSameDoor(t *testing.T) {
	r := newRig(t, 60, 12)
	ctx := context.Background()
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	tapped := r.tap(t, key, "$40.00", "Librería")
	agent := func(paid money.Cents, on civil.Date) captures.Capture {
		t.Helper()
		c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: paid, Merchant: "LIBRERIA",
			Day: on, Channel: store.ChannelAgent})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	if c := agent(4000, day); !c.AlreadyNoted || c.ID != tapped || c.Source != store.ChannelShortcut {
		t.Errorf("the assistant on the shortcut's day = %+v; want the shortcut's purchase, already noted", c)
	}
	read := agent(1999, day.AddDays(-5))
	if read.AlreadyNoted || read.Source != store.ChannelAgent || read.State != store.CapturePending {
		t.Fatalf("the assistant alone = %+v; want a purchase of its own", read)
	}
	if e := read.AsEntry(); e.BookedAt != nil || e.CaptureSource != store.ChannelAgent {
		t.Errorf("as a movement = %+v; want no time of day", e)
	}
	if e, err := r.db.Q().CaptureAsEntry(ctx, r.user, read.ID); err != nil || e.BookedAt != nil {
		t.Errorf("listed = %+v, %v; want no time of day", e, err)
	}
	// The owner typing it too is another channel, so it joins. The assistant
	// telling of it again — it reads the same purchases in transit each time
	// it looks — is the same purchase; another merchant on that day for the
	// same amount is a second one.
	if c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: 1999, Merchant: "Libros",
		Day: day.AddDays(-5)}); err != nil || !c.AlreadyNoted || c.ID != read.ID {
		t.Errorf("typed after the assistant = %+v, %v; want the assistant's purchase", c, err)
	}
	if c := agent(1999, day.AddDays(-5)); !c.AlreadyNoted || c.ID != read.ID {
		t.Errorf("the assistant twice = %+v; want its first purchase, already noted", c)
	}
	if c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: 1999, Merchant: "PAPELERIA",
		Day: day.AddDays(-5), Channel: store.ChannelAgent}); err != nil || c.AlreadyNoted {
		t.Errorf("the assistant on another merchant = %+v, %v; want a second purchase", c, err)
	}
	if channels := r.channels(t, read.ID); fmt.Sprint(channels) != "[agent manual]" {
		t.Errorf("signals = %v", channels)
	}

	// No other channel comes in this way, and asking is the caller's mistake,
	// said in Spanish, never a server error.
	for _, channel := range []string{store.ChannelShortcut, store.ChannelEmail, store.ChannelNotification, "bank"} {
		_, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: 100, Merchant: "x", Day: day,
			Channel: channel})
		if !errors.Is(err, captures.ErrChannel) {
			t.Errorf("Add by %q = %v, want ErrChannel", channel, err)
		}
		var refused *httpx.Error
		if !errors.As(captures.TranslateError(err), &refused) || refused.Status != http.StatusUnprocessableEntity ||
			refused.Code != "capture_channel" || refused.Message != "Una compra se anota a mano o desde el asistente." {
			t.Errorf("Add by %q answers %+v; want a 422 in Spanish", channel, refused)
		}
	}
	if n := r.count(t, card); n != 3 {
		t.Errorf("purchases = %d, want 3", n)
	}
}

// The assistant reads the same purchases in transit each time it looks. The
// same amount, merchant and day in the account is the purchase it told of
// before, whatever became of it — corrected by the owner, discarded, or joined
// to the shortcut's tap of the next day — and never a second one.
func TestTheAssistantTellingAgainIsTheSamePurchase(t *testing.T) {
	r := newRig(t, 60, 12)
	ctx := context.Background()
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	agent := func(paid money.Cents, merchant string, on civil.Date) captures.Capture {
		t.Helper()
		c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: paid, Merchant: merchant,
			Day: on, Channel: store.ChannelAgent})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	first := agent(1250, "SUPER 99  VIA ESPAÑA", day.AddDays(-3))
	amount, merchant := money.Cents(1300), "Super 99"
	if _, err := r.svc.Edit(ctx, r.user, first.ID, captures.EditInput{Amount: &amount, Merchant: &merchant}); err != nil {
		t.Fatal(err)
	}
	if c := agent(1250, "super 99 via españa", day.AddDays(-3)); !c.AlreadyNoted || c.ID != first.ID || c.Amount != -1300 {
		t.Errorf("told again after the owner corrected it = %+v; want it as corrected, already noted", c)
	}

	discarded := agent(800, "KIOSCO", day.AddDays(-2))
	if _, err := r.svc.Discard(ctx, r.user, discarded.ID); err != nil {
		t.Fatal(err)
	}
	if c := agent(800, "KIOSCO", day.AddDays(-2)); !c.AlreadyNoted || c.ID != discarded.ID ||
		c.State != store.CaptureDiscarded {
		t.Errorf("told again after the owner discarded it = %+v; want it still discarded", c)
	}

	tapped := r.tap(t, key, "$45.00", "Farmacia")
	if c := agent(4500, "FARMACIA DE TURNO", day.AddDays(-1)); !c.AlreadyNoted || c.ID != tapped {
		t.Errorf("the assistant beside the tap = %+v; want the tap", c)
	}
	if c := agent(4500, "FARMACIA DE TURNO", day.AddDays(-1)); !c.AlreadyNoted || c.ID != tapped {
		t.Errorf("told again beside the tap = %+v; want the tap", c)
	}

	// Another day is another purchase.
	if c := agent(1250, "SUPER 99 VIA ESPAÑA", day.AddDays(-2)); c.AlreadyNoted {
		t.Errorf("the same purchase on another day = %+v; want a new one", c)
	}
	if n := r.count(t, card); n != 4 {
		t.Errorf("purchases = %d, want 4", n)
	}
}

// A form repeats its first try with the same id when the answer was lost.
// When that try found the purchase already noted, the repeat finds the same
// one again rather than note it after all.
func TestRepeatingAnAddThatFoundItsPurchaseFindsItAgain(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	tapped := r.tap(t, key, "$60.00", "Óptica")
	r.now = lateNight.Add(time.Minute)
	id := uuid.New()
	body := fmt.Sprintf(`{"id": %q, "account_id": %q, "amount": "60.00", "merchant": "Lentes", "day": %q}`, id, card.ID, day)
	jwt := r.bearer(t, r.user)

	for try := range 3 {
		rec := r.serve(t, jwt, http.MethodPost, "/api/captures", body)
		var v struct {
			ID           uuid.UUID `json:"id"`
			AlreadyNoted bool      `json:"already_noted"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != http.StatusOK || v.ID != tapped || !v.AlreadyNoted {
			t.Errorf("try %d = %d %s; want the shortcut's purchase, already noted", try+1, rec.Code, rec.Body)
		}
	}
	if n := r.count(t, card); n != 1 {
		t.Errorf("purchases = %d, want 1", n)
	}
	if channels := r.channels(t, tapped); fmt.Sprint(channels) != "[shortcut manual]" {
		t.Errorf("signals = %v, want one typed", channels)
	}
	// The id is found by the signal's caller id, and kept in what it sent.
	if s := r.signal(t, tapped, store.ChannelManual); s.CallerID == nil || *s.CallerID != id || s.Raw["id"] != id.String() {
		t.Errorf("the typed signal = %+v; want it to carry the form's id", s)
	}
	// Another user cannot find it by that id.
	other := r.newUser(t, "b@example.com")
	theirs := r.account(t, other, "credit_card")
	c, err := r.svc.Add(context.Background(), other, captures.AddInput{ID: id, AccountID: theirs.ID, Amount: 6000,
		Merchant: "Lentes", Day: day})
	if err != nil || c.ID != id || c.AlreadyNoted || c.UserID != other {
		t.Errorf("their Add with the id = %+v, %v; want a purchase of theirs", c.Capture, err)
	}
	// A new form, with a new id, is another purchase.
	if rec := r.serve(t, jwt, http.MethodPost, "/api/captures",
		fmt.Sprintf(`{"id": %q, "account_id": %q, "amount": "60.00", "merchant": "Lentes", "day": %q}`, uuid.New(), card.ID, day)); rec.Code != http.StatusCreated {
		t.Errorf("a new form = %d %s; want a purchase of its own", rec.Code, rec.Body)
	}
}

// Whoever holds a key must not learn that a purchase was already noted: the
// reply is the same, byte for byte, whether the shortcut noted it or joined
// it, and so is every reply after it.
func TestTheShortcutCannotTellAJoinFromANewPurchase(t *testing.T) {
	typed, fresh := newRig(t, 60, 12), newRig(t, 60, 12)
	rigs := []*rig{typed, fresh}
	keys, cards := make([]string, 2), make([]store.Account, 2)
	for i, r := range rigs {
		cards[i] = r.account(t, r.user, "credit_card")
		keys[i] = "Bearer " + r.createKey(t, r.user, cards[i].ID)
	}
	// The owner typed it the day before, under another name.
	noted := typed.add(t, cards[0], day.AddDays(-1), 950, "Kiosco del parque")
	body := `{"amount": "$9.50", "merchant": "Kiosco", "card": "Visa Oro"}`

	send := func(step string) (ids []uuid.UUID) {
		t.Helper()
		var recs []*httptest.ResponseRecorder
		for i, r := range rigs {
			rec := r.post(t, keys[i], "application/json", body)
			recs = append(recs, rec)
			var id uuid.UUID
			if got := decodeReply(t, rec); got.Capture != nil {
				id = got.Capture.ID
			}
			ids = append(ids, id)
		}
		a, b := recs[0], recs[1]
		// A purchase's id is random either way; anything else differing
		// would tell.
		bodyA := bytes.ReplaceAll(a.Body.Bytes(), []byte(ids[0].String()), []byte("<id>"))
		bodyB := bytes.ReplaceAll(b.Body.Bytes(), []byte(ids[1].String()), []byte("<id>"))
		if a.Code != b.Code || a.Header().Get("Content-Type") != b.Header().Get("Content-Type") || !bytes.Equal(bodyA, bodyB) {
			t.Errorf("%s: joined %d %s\nnew    %d %s", step, a.Code, a.Body, b.Code, b.Body)
		}
		return ids
	}

	ids := send("the first tap")
	if ids[0] != noted.ID {
		t.Errorf("the shortcut noted %s; want it to join %s", ids[0], noted.ID)
	}
	// A repeat within two minutes is the same tap, joined or not.
	for _, r := range rigs {
		r.now = lateNight.Add(30 * time.Second)
	}
	send("a repeat")
	// Past two minutes it is another purchase, joined before or not.
	for _, r := range rigs {
		r.now = lateNight.Add(150 * time.Second)
	}
	send("another tap")
	for i, r := range rigs {
		if n := r.count(t, cards[i]); n != 2 {
			t.Errorf("rig %d: purchases = %d, want 2", i, n)
		}
	}
}

// The owner discarding a tap and then typing a purchase of the same amount on
// its day is saying it happened after all: it is a new purchase that counts,
// never joined to the discarded one, where it would count for nothing. The
// bank's email of the declined tap still joins the discarded one.
func TestTypingAPurchaseTheOwnerDiscardedNotesANewOne(t *testing.T) {
	r := newRig(t, 60, 12)
	ctx := context.Background()
	for i, channel := range []string{store.ChannelManual, store.ChannelAgent} {
		card := r.account(t, r.user, "credit_card")
		key := "Bearer " + r.createKey(t, r.user, card.ID)
		r.now = lateNight.Add(time.Duration(i-3) * time.Hour)
		declined := r.tap(t, key, "$18.00", "Farmacia")
		if _, err := r.svc.Discard(ctx, r.user, declined); err != nil {
			t.Fatal(err)
		}

		c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: 1800, Merchant: "Farmacia",
			Day: day, Channel: channel})
		if err != nil || c.AlreadyNoted || c.ID == declined || c.Source != channel || c.State != store.CapturePending {
			t.Errorf("%s after the discard = %+v, %v; want a new purchase that counts", channel, c, err)
		}
		if got := r.get(t, r.user, declined); got.State != store.CaptureDiscarded {
			t.Errorf("%s: the discarded purchase is %s now", channel, got.State)
		}
		if got, joined := r.email(t, card, 1800, r.now.Add(5*time.Minute)); !joined || got.ID != declined {
			t.Errorf("%s: the email of the declined tap = %s, %v; want it to join %s", channel, got.ID, joined, declined)
		}
		if n := r.count(t, card); n != 2 {
			t.Errorf("%s: purchases = %d, want the discarded one and the new one", channel, n)
		}
	}
	if spent := r.spent(t); spent != 2*1800 {
		t.Errorf("spent = %s, want 36.00: each typed purchase once", spent)
	}
}

// What the owner or the assistant typed with a purchase already noted is
// kept in its signal, and fills in what the purchase lacks while it is open:
// a note when it has none, and a category chosen over a rule's or none. A
// note it has, a category someone chose, and its amount, day, merchant and
// state stay as they were.
func TestWhatWasTypedFillsInWhatThePurchaseLacks(t *testing.T) {
	r := newRig(t, 60, 12)
	ctx := context.Background()
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	food, health := r.category(t, r.user, "Alimentación"), r.category(t, r.user, "Salud")
	r.rule(t, "super", food)
	r.now = lateNight.Add(-10 * time.Hour)
	type typed struct {
		category *uuid.UUID
		note     string
	}

	for _, tt := range []struct {
		name         string
		merchant     string
		chosen       *uuid.UUID // the purchase's category chosen by the owner before
		noteBefore   string
		typed        typed
		wantCategory *uuid.UUID
		wantSource   string
		wantNote     string
	}{
		{"over a rule's category, with no note", "Super 99", nil, "", typed{&health, "pan"}, &health, "assistant", "pan"},
		{"with no category", "Kiosco", nil, "", typed{&health, ""}, &health, "assistant", ""},
		{"under a category someone chose", "Kiosco", &food, "", typed{&health, "pan"}, &food, "user", "pan"},
		{"beside a note it has", "Super 99", nil, "del mes", typed{nil, "pan"}, &food, "rule", "del mes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r.now = r.now.Add(time.Hour) // a tap of its own, on day
			paid := money.Cents(1000 + r.count(t, card))
			tapped := r.tap(t, key, paid.String(), tt.merchant)
			if tt.chosen != nil {
				if err := r.db.Q().SetCaptureCategory(ctx, r.user, tapped, tt.chosen, "user"); err != nil {
					t.Fatal(err)
				}
			}
			if tt.noteBefore != "" {
				if err := r.db.Q().SetCaptureNote(ctx, r.user, tapped, tt.noteBefore); err != nil {
					t.Fatal(err)
				}
			}
			before := r.get(t, r.user, tapped)

			c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: paid, Merchant: "Lo que sea",
				Day: day, CategoryID: tt.typed.category, Note: tt.typed.note, Channel: store.ChannelAgent})
			if err != nil || !c.AlreadyNoted || c.ID != tapped {
				t.Fatalf("Add = %+v, %v; want the shortcut's purchase", c, err)
			}
			after := r.get(t, r.user, tapped)
			if !sameCategory(after.CategoryID, tt.wantCategory) || after.CategorySource == nil ||
				*after.CategorySource != tt.wantSource || after.Note != tt.wantNote {
				t.Errorf("category %v (%v), note %q; want %v (%s), %q", after.CategoryID, after.CategorySource,
					after.Note, tt.wantCategory, tt.wantSource, tt.wantNote)
			}
			if after.Amount != before.Amount || after.BookedOn != before.BookedOn || after.Merchant != before.Merchant ||
				after.State != before.State || after.Source != before.Source || !after.ReceivedAt.Equal(before.ReceivedAt) {
				t.Errorf("after the Add: %+v\nbefore: %+v", after, before)
			}

			wantRaw := map[string]string{"merchant": "Lo que sea", "amount": paid.String(), "day": day.String()}
			if tt.typed.category != nil {
				wantRaw["category_id"] = tt.typed.category.String()
			}
			if tt.typed.note != "" {
				wantRaw["note"] = tt.typed.note
			}
			if raw := r.signal(t, tapped, store.ChannelAgent).Raw; fmt.Sprint(raw) != fmt.Sprint(wantRaw) {
				t.Errorf("the assistant's signal sent %v; want %v", raw, wantRaw)
			}
		})
	}

	// A linked purchase is its line now: what was typed stays in the signal.
	r.now = r.now.Add(time.Hour)
	r.line(t, card, day, -4321, store.KindExpense, "SUPER 99")
	linked := r.tap(t, key, "$43.21", "Super 99")
	before := r.get(t, r.user, linked)
	if before.State != store.CaptureLinked {
		t.Fatalf("fixture: %s; want linked", before.State)
	}
	c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: 4321, Merchant: "Super",
		Day: day, CategoryID: &health, Note: "pan"})
	if err != nil || !c.AlreadyNoted || c.ID != linked {
		t.Fatalf("Add = %+v, %v; want the linked purchase", c, err)
	}
	if after := r.get(t, r.user, linked); after.Note != "" || !sameCategory(after.CategoryID, before.CategoryID) ||
		!after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("the linked purchase changed: %+v", after)
	}
	if raw := r.signal(t, linked, store.ChannelManual).Raw; raw["note"] != "pan" || raw["category_id"] != health.String() ||
		raw["merchant"] != "Super" {
		t.Errorf("the typed signal sent %v; want what was typed kept", raw)
	}
}

// The windows are pinned: widening either joins more real purchases into
// one, narrowing either lets one purchase count twice. At its edge each still
// joins, and a second past it does not.
func TestAnotherChannelJoinsWithinAnHourOrADay(t *testing.T) {
	if captures.JoinWindow != time.Hour || captures.JoinDays != 1 {
		t.Fatalf("JoinWindow = %s, JoinDays = %d; want an hour and a day", captures.JoinWindow, captures.JoinDays)
	}
	r := newRig(t, 60, 12)
	ctx := context.Background()

	// An email at 00:10 tells of a tap on the evening before within the
	// window, ahead of one on its own day hours away: it is the time that
	// decides. A second past the window, the day does.
	email := lateNight.Add(40 * time.Minute)
	for _, tt := range []struct {
		before   time.Duration
		wantLate bool
	}{{captures.JoinWindow, true}, {captures.JoinWindow + time.Second, false}} {
		card := r.account(t, r.user, "credit_card")
		key := "Bearer " + r.createKey(t, r.user, card.ID)
		r.now = email.Add(-tt.before)
		late := r.tap(t, key, "$20.00", "Bar")
		r.now = email.Add(8 * time.Hour)
		morning := r.tap(t, key, "$20.00", "Panadería")
		want := morning
		if tt.wantLate {
			want = late
		}
		if got, joined := r.email(t, card, 2000, email); !joined || got.ID != want {
			t.Errorf("a tap %s before: the email joined %s, %v; want %s", tt.before, got.ID, joined, want)
		}
	}

	// A purchase typed within JoinDays of the shortcut's day is the same one.
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	for days, want := range map[int]bool{-captures.JoinDays - 1: false, -captures.JoinDays: true, 0: true,
		captures.JoinDays: true, captures.JoinDays + 1: false} {
		paid := money.Cents(5000 + days)
		r.now = lateNight
		tapped := r.tap(t, key, paid.String(), "Tienda")
		r.now = lateNight.Add(72 * time.Hour) // so the days after are not ahead of today
		c, err := r.svc.Add(ctx, r.user, captures.AddInput{AccountID: card.ID, Amount: paid, Merchant: "Tienda",
			Day: day.AddDays(days)})
		if err != nil || c.AlreadyNoted != want || (want && c.ID != tapped) {
			t.Errorf("typed %d days from the tap = %+v, %v; want already noted %v", days, c.Capture, err, want)
		}
	}
}

// --- fixtures ----------------------------------------------------------------

func sameCategory(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// allowEmail lets a purchase be noted by email first. The bank's email has no
// endpoint yet, so captures cannot come from it until its own migration; the
// test's database takes it now, as that migration will.
func (f fixture) allowEmail(t *testing.T) {
	t.Helper()
	if err := storetest.Exec(t, f.db, `ALTER TABLE captures DROP CONSTRAINT captures_source_check,
		ADD CONSTRAINT captures_source_check CHECK (source IN ('shortcut', 'manual', 'agent', 'email'))`); err != nil {
		t.Fatal(err)
	}
}

// email tells of a purchase as the bank's email will once it is read: under
// the user's lock, joined to the purchase it tells of or, when there is none,
// noted with it as its first signal (which needs allowEmail).
func (f fixture) email(t *testing.T, a store.Account, paid money.Cents, at time.Time) (c store.Capture, joined bool) {
	t.Helper()
	ctx := context.Background()
	err := f.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, a.UserID); err != nil {
			return err
		}
		day := civil.Of(at.In(civil.Panama))
		raw := map[string]string{"merchant": "COMERCIO DE PRUEBA"}
		var err error
		c, joined, err = captures.JoinSignal(ctx, q, a.UserID, a.ID, paid, day,
			store.CaptureSignal{Channel: store.ChannelEmail, At: at, Raw: raw})
		if err != nil || joined {
			return err
		}
		c = store.Capture{ID: uuid.New(), UserID: a.UserID, AccountID: a.ID, Source: store.ChannelEmail,
			ReceivedAt: at, BookedOn: day, Amount: -paid, Merchant: raw["merchant"], Raw: raw}
		if err := q.InsertCapture(ctx, c); err != nil {
			return err
		}
		c, err = q.CaptureByID(ctx, a.UserID, c.ID)
		return err
	})
	if err != nil {
		t.Fatalf("email: %v", err)
	}
	return c, joined
}

// channels lists, in order, the channels that told of one of the owner's
// purchases.
func (f fixture) channels(t *testing.T, id uuid.UUID) []string {
	t.Helper()
	signals, err := f.db.Q().CaptureSignals(context.Background(), f.user, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range signals {
		out = append(out, s.Channel)
	}
	return out
}

// signal is the signal of one channel on one of the owner's purchases.
func (f fixture) signal(t *testing.T, id uuid.UUID, channel string) store.CaptureSignal {
	t.Helper()
	signals, err := f.db.Q().CaptureSignals(context.Background(), f.user, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range signals {
		if s.Channel == channel {
			return s
		}
	}
	t.Fatalf("no %s signal among %+v", channel, signals)
	return store.CaptureSignal{}
}

// tap sends a purchase through the shortcut at the rig's now, and returns the
// purchase it was noted as.
func (r *rig) tap(t *testing.T, key, amount, merchant string) uuid.UUID {
	t.Helper()
	rec := r.post(t, key, "application/json", fmt.Sprintf(`{"amount": %q, "merchant": %q}`, amount, merchant))
	got := decodeReply(t, rec)
	if rec.Code != http.StatusCreated || got.Status != captures.IngestNoted || got.Capture == nil {
		t.Fatalf("tap %s at %s = %d %s", amount, merchant, rec.Code, rec.Body)
	}
	return got.Capture.ID
}
