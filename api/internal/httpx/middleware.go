package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/logging"
)

type requestIDKey struct{}

// RequestIDHeader is the header the id is read from and echoed back on.
const RequestIDHeader = "X-Request-Id"

// RequestIDFromContext returns the current request's id, or "" outside a request.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// RequestID assigns every request a correlation id and echoes it back.
//
// An id supplied by the client is honoured so a trace can span the browser and
// the API, but it is length-capped and stripped of anything unprintable before
// being copied into logs and response headers — an unbounded attacker-supplied
// string in a log line is a log-injection primitive, not a convenience.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitiseRequestID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = uuid.NewString()
		}

		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sanitiseRequestID(raw string) string {
	const maxLen = 64
	if len(raw) > maxLen {
		raw = raw[:maxLen]
	}
	var b strings.Builder
	for _, r := range raw {
		// Deliberately narrow: enough for a UUID or a trace id, and nothing
		// that could forge a new log line or a header.
		if r == '-' || r == '_' || r == '.' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// accessLog is what a request's line records beyond the request itself,
// filled in by the layers below it.
type accessLog struct {
	// redactPath logs the route's pattern instead of the path.
	redactPath bool
}

type accessLogKey struct{}

// RedactPath makes the access log record the route's pattern — PUT
// /api/mcp/uploads/{token} — instead of the request's path, for a route whose
// path is itself a secret. It goes on the router of those routes, so a request
// there that matches none of them is redacted too.
func RedactPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if log, ok := r.Context().Value(accessLogKey{}).(*accessLog); ok {
			log.redactPath = true
		}
		next.ServeHTTP(w, r)
	})
}

// RequestLogger logs one line per request and puts a request-scoped logger in
// the context, so everything logged downstream carries the same id without any
// layer having to thread it through by hand.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLogger := logger.With("request_id", RequestIDFromContext(r.Context()))
			log := &accessLog{}
			ctx := context.WithValue(logging.WithLogger(r.Context(), reqLogger), accessLogKey{}, log)

			// chi's wrapper records the status and byte count while still
			// forwarding Flush, which the chat's SSE stream depends on.
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			next.ServeHTTP(ww, r.WithContext(ctx))

			path := r.URL.Path
			if log.redactPath {
				path = "(redacted)"
				if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
					path = rctx.RoutePattern()
				}
			}
			attrs := []any{
				"method", r.Method,
				"path", path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_ip", ClientIP(r),
			}
			// A 5xx is already logged with its cause by Fail; this line is the
			// access record, so it stays at info and does not duplicate detail.
			switch {
			case ww.Status() >= http.StatusInternalServerError:
				reqLogger.Error("request", attrs...)
			case ww.Status() >= http.StatusBadRequest:
				reqLogger.Warn("request", attrs...)
			default:
				reqLogger.Info("request", attrs...)
			}
		})
	}
}

// Recoverer turns a panic into a 500 instead of a dropped connection, and logs
// the stack so the bug is diagnosable.
//
// It re-panics on http.ErrAbortHandler, which is the documented way to abandon
// a response on purpose and must keep working.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			logging.FromContext(r.Context()).Error("panic recovered",
				"panic", rec, "stack", string(debug.Stack()))
			Fail(w, r, Internal(errPanic))
		}()
		next.ServeHTTP(w, r)
	})
}

// errPanic keeps the recovered value out of the response while still giving the
// mapper something to wrap.
var errPanic = &Error{
	Status: http.StatusInternalServerError, Code: "internal_error",
	Message: "Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.",
}

// SecurityHeaders sets the headers that are cheap and unconditionally correct
// for a JSON API. The frontend is served by nginx, which sets its own
// content-security policy for the HTML; these apply to API responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// Nothing the API returns should ever be cached: it is all
		// account-specific and some of it is a session token.
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// CORS allows the listed origins, with credentials, for the browser dev server.
//
// In the shipped stack nginx serves the SPA and proxies /api on the same
// origin, so no CORS is involved at all; this exists for `npm run dev`. Origins
// are matched exactly against an allowlist and reflected — never "*" — because
// "*" is incompatible with credentialed requests and would defeat the point.
func CORS(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && slices.Contains(origins, origin) {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Headers",
					"Authorization, Content-Type, Idempotency-Key, "+RequestIDHeader)
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Expose-Headers", RequestIDHeader)
				h.Set("Access-Control-Max-Age", "600")
				// Responses vary by origin, so a shared cache must not serve
				// one origin's response to another.
				h.Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP is the address used for rate limiting.
//
// Every deployment puts exactly one proxy in front of this API — Traefik in
// production, the web container's nginx locally — and that proxy appends the
// address it accepted the connection from to X-Forwarded-For. So only the last
// entry is trusted. Everything before it arrived with the request, written by
// whoever sent it: keying a limit on the first entry, as this once did, let
// anyone reset every per-address limit by changing a header. Without the
// header, the peer is RemoteAddr itself.
//
// That one proxy is the trust boundary, stated here rather than assumed.
// Reached directly, the API would take a client's own last entry at its word;
// behind a second proxy, every client would share the first one's address.
func ClientIP(r *http.Request) string {
	// A header sent on several lines is one list (RFC 9110 §5.3), so the last
	// line holds the last entry.
	if lines := r.Header.Values("X-Forwarded-For"); len(lines) > 0 {
		last := lines[len(lines)-1]
		if i := strings.LastIndexByte(last, ','); i >= 0 {
			last = last[i+1:]
		}
		if ip := strings.TrimSpace(last); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
