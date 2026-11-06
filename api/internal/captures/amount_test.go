package captures_test

import (
	"errors"
	"testing"

	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

func TestParseWalletAmount(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want money.Cents
	}{
		// How Wallet may write a dollar amount, in any locale.
		{"$4.50", 450},
		{"US$4.50", 450},
		{"us$4.50", 450},
		{"U$S 4.50", 450},
		{"USD 4.50", 450},
		{"4.50 USD", 450},
		{"USD $4.50", 450},
		{"B/.4.50", 450},
		{"B/. 4.50", 450},
		{"B/4.50", 450},
		{"PAB 4.50", 450},
		{"4,50", 450},
		{"4,50 $", 450},
		{"4.5", 450},
		{"+$4.50", 450},
		{"  $4.50\n", 450},
		{"$\u00a04.50", 450},
		{"US$\u202f4.50", 450},
		{"$0.05", 5},
		{"$12", 1200},
		{"00.50", 50},
		// Thousands, written every way.
		{"1,234.56", 123456},
		{"1.234,56", 123456},
		{"1 234,56", 123456},
		{"1\u202f234,56", 123456},
		{"1'234.56", 123456},
		{"1\u2019234.56", 123456},
		{"1 234", 123400},
		{"1'234", 123400},
		{"12 345,67", 1234567},
		{"$1,234.5", 123450},
		{"1,234", 123400},
		{"1.234", 123400},
		{"12,345", 1234500},
		{"50,000.00", 5000000}, // the most a tap can be
		{"50.000,00", 5000000},
	} {
		got, err := captures.ParseWalletAmount(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("ParseWalletAmount(%q) = %d, %v; want %d", tt.raw, got, err, tt.want)
		}
	}

	for _, tt := range []struct {
		raw  string
		want error
	}{
		{"", captures.ErrNoAmount},
		{"   ", captures.ErrNoAmount},
		{"$", captures.ErrNoAmount},
		{"$0.00", captures.ErrNoAmount},
		{"0", captures.ErrNoAmount},
		{"0,00 USD", captures.ErrNoAmount},
		{"-$5.00", captures.ErrRefund},
		{"\u2212$5.00", captures.ErrRefund},
		{"$-5.00", captures.ErrRefund},
		{"($5.00)", captures.ErrRefund},
		{"$5.00-", captures.ErrRefund},
		{"- US$ 5.00", captures.ErrRefund},
		{"-5", captures.ErrRefund},
		{"-€5.00", captures.ErrRefund}, // a refund in any currency
		{"1,2345", captures.ErrMalformed},
		{"abc", captures.ErrMalformed},
		{"EUR", captures.ErrMalformed},
		{"4-50", captures.ErrMalformed},
		{"4.50.", captures.ErrMalformed},
		{".50", captures.ErrMalformed},
		{"$.50", captures.ErrMalformed},
		{"US$,50", captures.ErrMalformed},
		{"1234,567", captures.ErrMalformed},
		{"0,500", captures.ErrMalformed},
		{"1,234,56", captures.ErrMalformed},
		{"1.234,567", captures.ErrMalformed},
		{"12,34.5", captures.ErrMalformed},
		{"1,234.5,6", captures.ErrMalformed},
		{"4.50 #", captures.ErrMalformed},
		{"4.50 USD 3", captures.ErrMalformed},
		{"1e3", captures.ErrMalformed},
		// A space or apostrophe groups thousands or is a mistake: never
		// dropped to run the digits together a hundred times larger.
		{"4 50", captures.ErrMalformed},
		{"4'50", captures.ErrMalformed},
		{"12 5", captures.ErrMalformed},
		{"$4 50", captures.ErrMalformed},
		{"1  234", captures.ErrMalformed},
		{"1'234 567", captures.ErrMalformed},
		{"1 234.567", captures.ErrMalformed},
		{"4.5 0", captures.ErrMalformed},
		{"4 ,50", captures.ErrMalformed},
		// A capture key pasted into the field is no amount, whatever hyphens
		// it has beside its digits: never read as a refund.
		{"chn_-7kQ_x-", captures.ErrMalformed},
		{"chn_Qx-9-", captures.ErrMalformed},
		{"chn_-9", captures.ErrMalformed},
		{"chn_-abc-", captures.ErrMalformed},
		{"-chn_4", captures.ErrMalformed},
		{"dólares 4.50", captures.ErrMalformed}, // no currency is that long
		{"$50,000.01", captures.ErrImplausible},
		{"84000", captures.ErrImplausible},
		{"4111111111111111", captures.ErrImplausible},
		{"99999999999999999999999", captures.ErrImplausible},
	} {
		if got, err := captures.ParseWalletAmount(tt.raw); !errors.Is(err, tt.want) {
			t.Errorf("ParseWalletAmount(%q) = %d, %v; want %v", tt.raw, got, err, tt.want)
		}
	}
}

// A JSON number has one meaning in any locale: '.' is its decimal mark and
// nothing groups thousands, so "12.345" is never twelve thousand dollars.
func TestParseNumberAmount(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want money.Cents
	}{
		{"4.5", 450},
		{"4.50", 450},
		{"12", 1200},
		{"0.05", 5},
		{"1234.5", 123450},
		{"50000", 5000000},
	} {
		got, err := captures.ParseNumberAmount(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("ParseNumberAmount(%q) = %d, %v; want %d", tt.raw, got, err, tt.want)
		}
	}

	for _, tt := range []struct {
		raw  string
		want error
	}{
		{"12.345", captures.ErrMalformed}, // $24.69 split in two
		{"1.234", captures.ErrMalformed},
		{"4.500", captures.ErrMalformed},
		{"1e3", captures.ErrMalformed},
		{"1E+2", captures.ErrMalformed},
		{"4.", captures.ErrMalformed},
		{".5", captures.ErrMalformed},
		{"", captures.ErrMalformed},
		{"0", captures.ErrNoAmount},
		{"0.00", captures.ErrNoAmount},
		{"-0", captures.ErrNoAmount},
		{"-5", captures.ErrRefund},
		{"-4.5", captures.ErrRefund},
		{"-84000", captures.ErrRefund},
		{"50000.01", captures.ErrImplausible},
		{"99999999999999999999999", captures.ErrImplausible},
	} {
		if got, err := captures.ParseNumberAmount(tt.raw); !errors.Is(err, tt.want) {
			t.Errorf("ParseNumberAmount(%q) = %d, %v; want %v", tt.raw, got, err, tt.want)
		}
	}
}

func TestAForeignAmountSaysItsCurrency(t *testing.T) {
	for raw, want := range map[string]string{
		"€8.00":         "EUR",
		"8,00 €":        "EUR",
		"£3.20":         "GBP",
		"ARS 23,400.00": "ARS",
		"23.400,00 ars": "ARS",
		"MX$120.00":     "MX$",
		"R$ 15,90":      "R$",
		"S/. 12.00":     "S/.",
		"$4.50 EUR":     "EUR",
	} {
		_, err := captures.ParseWalletAmount(raw)
		var foreign captures.ForeignCurrencyError
		if !errors.Is(err, captures.ErrForeignCurrency) || !errors.As(err, &foreign) || foreign.Code != want {
			t.Errorf("ParseWalletAmount(%q) = %v; want ErrForeignCurrency %s", raw, err, want)
		}
	}
}

func TestAnImplausibleAmountSaysWhatWasRead(t *testing.T) {
	_, err := captures.ParseWalletAmount("US$84,000.00")
	var implausible captures.ImplausibleError
	if !errors.As(err, &implausible) || implausible.Amount != 8_400_000 {
		t.Errorf("ParseWalletAmount = %v; want ImplausibleError 84000.00", err)
	}
}
