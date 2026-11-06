package plan_test

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
)

// A save that is wrong is refused before it reaches the database, so these
// need none.
func offline() *plan.Service {
	svc := plan.NewService(nil)
	svc.SetNow(func() time.Time { return time.Date(2026, 11, 6, 9, 0, 0, 0, civil.Panama) })
	return svc
}

func TestASaveSaysEveryFieldThatIsWrongAtOnce(t *testing.T) {
	nov := plan.Month{Year: 2026, Month: time.November}
	account := uuid.New()
	target := money.Cents(-5)
	cases := []struct {
		name string
		in   plan.ItemInput
		want map[string]string
	}{
		{
			name: "a payment with nothing filled in",
			in:   plan.ItemInput{Kind: plan.KindFixed, Month: nov},
			want: map[string]string{
				"scope":  "Elige si vale desde este mes o solo para este mes.",
				"name":   "Ponle un nombre de hasta 60 caracteres.",
				"amount": "Indica un monto mayor que cero.",
				"day":    "Elige un día del 1 al 31.",
			},
		},
		{
			name: "a paycheck with too much of everything",
			in: plan.ItemInput{Kind: plan.KindIncome, Month: nov, Scope: plan.ScopeFrom,
				Name: strings.Repeat("Sueldo ", 9), Amount: 65000, Day: 32,
				MatchText: "ACH EMPRESA X ACH EMPRESA X ACH EMPRESA X ACH EMPRESA X ACH EMPRESA X",
				AccountID: &account, TargetMonth: nov},
			want: map[string]string{
				"name":       "Ponle un nombre de hasta 60 caracteres.",
				"day":        "Elige un día del 1 al 31.",
				"match_text": "El texto para reconocerlo admite hasta 60 caracteres.",
				"account_id": "Solo un ahorro va ligado a una cuenta.",
				"target":     "Solo un ahorro tiene una meta.",
			},
		},
		{
			name: "a saving recognised like a payment",
			in: plan.ItemInput{Kind: plan.KindSaving, Month: nov, Scope: plan.ScopeOnly, Name: "Fondo​",
				Amount: 15000, MatchText: "TRANSFERENCIA", CategoryID: &account, Target: &target},
			want: map[string]string{
				"name":        "El nombre tiene caracteres que no se pueden mostrar.",
				"match_text":  "Un ahorro se reconoce por su cuenta, no por un texto.",
				"category_id": "Un ahorro no lleva categoría.",
				"target":      "Indica una meta mayor que cero.",
			},
		},
		{
			name: "a planned purchase, which is not kept yet",
			in: plan.ItemInput{Kind: plan.KindPurchase, Month: nov, Scope: plan.ScopeFrom, Name: "Cámara",
				Amount: 110000},
			want: map[string]string{"kind": "Las compras planeadas todavía no se pueden guardar."},
		},
		{
			name: "something that is no kind of item",
			in:   plan.ItemInput{Kind: "gasto", Month: nov, Scope: plan.ScopeFrom, Name: "Algo", Amount: 100},
			want: map[string]string{"kind": "Elige si es un ingreso, un pago fijo o un ahorro."},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := offline().SaveItem(context.Background(), uuid.New(), uuid.New(), c.in)
			if got := plan.FieldProblems(err); !maps.Equal(got, c.want) {
				t.Errorf("FieldProblems = %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestACapNeedsAnAmountAndAScope(t *testing.T) {
	err := offline().SaveCap(context.Background(), uuid.New(), uuid.New(),
		plan.CapInput{Month: plan.Month{Year: 2026, Month: time.November}})
	want := map[string]string{
		"scope":  "Elige si vale desde este mes o solo para este mes.",
		"amount": "Indica un monto mayor que cero.",
	}
	if got := plan.FieldProblems(err); !maps.Equal(got, want) {
		t.Errorf("FieldProblems = %v, want %v", got, want)
	}
	if got := plan.FieldProblems(plan.ErrMonthPast); len(got) != 0 {
		t.Errorf("a past month is about no field, yet %v", got)
	}
}
