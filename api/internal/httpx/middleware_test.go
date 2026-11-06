package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JuanKsPty/chenchen/api/internal/httpx"
)

func TestClientIPIsTheAddressTheProxyAppended(t *testing.T) {
	// httptest's requests come from 192.0.2.1.
	for name, tt := range map[string]struct {
		forwarded []string
		want      string
	}{
		"no proxy":                     {nil, "192.0.2.1"},
		"the proxy's entry alone":      {[]string{"203.0.113.7"}, "203.0.113.7"},
		"spoofed entries before it":    {[]string{"10.0.0.1, 198.51.100.9,203.0.113.7"}, "203.0.113.7"},
		"spoofed on a line of its own": {[]string{"10.0.0.1, 198.51.100.9", "203.0.113.7"}, "203.0.113.7"},
		"spaces around it":             {[]string{"10.0.0.1 ,   203.0.113.7  "}, "203.0.113.7"},
		"an IPv6 peer":                 {[]string{"10.0.0.1, 2001:db8::7"}, "2001:db8::7"},
		"nothing after the last comma": {[]string{"203.0.113.7, "}, "192.0.2.1"},
		"an empty header":              {[]string{""}, "192.0.2.1"},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, line := range tt.forwarded {
				r.Header.Add("X-Forwarded-For", line)
			}
			if got := httpx.ClientIP(r); got != tt.want {
				t.Errorf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnAddressCannotDodgeItsLimitByWritingXForwardedFor(t *testing.T) {
	limited := httpx.RateLimit(httpx.NewRateLimiter(1), "Demasiados intentos.")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	// One client, through the one proxy, inventing a new first entry each time.
	for i, spoofed := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		r.Header.Set("X-Forwarded-For", spoofed+", 203.0.113.7")
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, r)

		want := http.StatusTooManyRequests
		if i == 0 {
			want = http.StatusNoContent
		}
		if rec.Code != want {
			t.Errorf("request %d, claiming to be %s = %d, want %d", i, spoofed, rec.Code, want)
		}
	}
}
