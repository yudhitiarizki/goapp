// Package logging is the application's structured logger. Every record is a
// JSON object carrying a "type" (api, adaptor, db, error) and the "request_id"
// taken from the context, so all work done for one HTTP request — the request
// itself, outbound gateway calls, DB queries and errors — lines up under the
// same id in Kibana Discover.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/yudhitiarizki/goapp/errs"
	"github.com/yudhitiarizki/goapp/reqctx"
)

// Log types. The "path" field means something slightly different per type:
//
//	api     -> the HTTP route path            (set by the gin middleware)
//	adaptor -> the outbound request path       (set by httpx)
//	db      -> the calling Go method name      (set by the GORM logger)
//	error   -> the HTTP path where it surfaced (plus a stack trace)
//	app     -> manual Info/Warn logs from service code
const (
	TypeAPI     = "api"
	TypeAdaptor = "adaptor"
	TypeDB      = "db"
	TypeError   = "error"
	TypeApp     = "app"
)

// Logger wraps slog with the per-type helpers above. Logging can be turned off
// entirely (enabled=false) or per type (disabled set), so e.g. db or adaptor
// logs can be silenced independently.
type Logger struct {
	sl       *slog.Logger
	enabled  bool
	disabled map[string]bool
}

// New builds a JSON logger writing to w. enabled is the master switch; disable
// lists log types to silence (e.g. "db", "adaptor"); unknown entries are ignored.
func New(w io.Writer, enabled bool, disable []string) *Logger {
	d := make(map[string]bool, len(disable))
	for _, t := range disable {
		if t = strings.TrimSpace(t); t != "" {
			d[t] = true
		}
	}
	return &Logger{
		sl:       slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})),
		enabled:  enabled,
		disabled: d,
	}
}

// On reports whether a given log type will be emitted. Callers may use it to
// skip expensive work (e.g. marshaling bodies) when a type is off.
func (l *Logger) On(typ string) bool {
	return l.enabled && !l.disabled[typ]
}

// FileWriter returns a writer for path, creating parent dirs, or stdout when
// path is empty or cannot be opened.
func FileWriter(path string) io.Writer {
	if path == "" {
		return os.Stdout
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return os.Stdout
	}
	return f
}

// Slog exposes the underlying slog.Logger for callers that want plain logging.
func (l *Logger) Slog() *slog.Logger { return l.sl }

// emit writes one record, always stamping type + request_id (+ user_id when
// known) from the context so every log line correlates.
func (l *Logger) emit(ctx context.Context, level slog.Level, typ, path, msg string, attrs []slog.Attr) {
	if !l.On(typ) {
		return
	}
	meta := reqctx.FromContext(ctx)
	base := make([]slog.Attr, 0, len(attrs)+4)
	base = append(base, slog.String("type", typ), slog.String("request_id", meta.RequestID))
	if path != "" {
		base = append(base, slog.String("path", path))
	}
	if meta.UserID != "" {
		base = append(base, slog.String("user_id", meta.UserID))
	}
	l.sl.LogAttrs(ctx, level, msg, append(base, attrs...)...)
}

// Adaptor logs an outbound call to an external system (type=adaptor).
func (l *Logger) Adaptor(ctx context.Context, path string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelInfo, TypeAdaptor, path, "outbound_request", attrs)
}

// DB logs a database query (type=db); method is the calling Go method.
func (l *Logger) DB(ctx context.Context, method string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelInfo, TypeDB, method, "db_query", attrs)
}

// Info logs a manual application message from service code (type=app).
func (l *Logger) Info(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelInfo, TypeApp, "", msg, attrs)
}

// Warn logs a manual warning from service code (type=app).
func (l *Logger) Warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.emit(ctx, slog.LevelWarn, TypeApp, "", msg, attrs)
}

// Error logs an error with a stack trace (type=error). If err was wrapped with
// errs.Wrap at its origin, the stack points there and error_caller is the origin
// file:line; otherwise the current stack is used as a fallback.
func (l *Logger) Error(ctx context.Context, err error, attrs ...slog.Attr) {
	caller, stack := stackOf(err)
	all := make([]slog.Attr, 0, len(attrs)+3)
	all = append(all, slog.String("error", err.Error()), slog.String("stack", stack))
	if caller != "" {
		all = append(all, slog.String("error_caller", caller))
	}
	all = append(all, attrs...)
	l.emit(ctx, slog.LevelError, TypeError, "", err.Error(), all)
}

// stackOf prefers the stack captured at the error's origin (errs.Wrap); when the
// error carries none, it falls back to the current goroutine stack.
func stackOf(err error) (caller, stack string) {
	if err != nil {
		if s := errs.Format(err); s != "" {
			return errs.Caller(err), s
		}
	}
	return "", string(debug.Stack())
}

// callerMethod walks the stack to the first frame outside gorm and this
// package, and returns a short "pkg.Func" name — used as the db log's path.
func callerMethod() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if f.Function != "" &&
			!strings.Contains(f.Function, "gorm.io/") &&
			!strings.Contains(f.Function, "/goapp/logging") {
			name := f.Function
			if i := strings.LastIndex(name, "/"); i >= 0 {
				name = name[i+1:]
			}
			return name
		}
		if !more {
			return ""
		}
	}
}
