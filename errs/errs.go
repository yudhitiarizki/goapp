// Package errs adds a captured stack trace to an error at the point it is
// created or first wrapped, so logs can show where the error actually came from
// (file:line) instead of where it was finally handled. Wrap at the origin:
//
//	if err != nil {
//	    return errs.Wrap(err)   // captures this call site + the frames below it
//	}
//
// The logging package reads this stack back when it logs a type=error record.
package errs

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

type stackErr struct {
	err error
	pcs []uintptr
}

func (e *stackErr) Error() string    { return e.err.Error() }
func (e *stackErr) Unwrap() error    { return e.err }
func (e *stackErr) stack() []uintptr { return e.pcs }

type stackCarrier interface{ stack() []uintptr }

func callers() []uintptr {
	pcs := make([]uintptr, 32)
	// skip: runtime.Callers, callers, the exported Wrap/New/Errorf -> start at
	// the real caller.
	n := runtime.Callers(3, pcs)
	return pcs[:n]
}

// Wrap attaches a stack to err if it does not already carry one, keeping the
// earliest (deepest) stack when wrapped multiple times. Returns nil for nil.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	var sc stackCarrier
	if errors.As(err, &sc) {
		return err
	}
	return &stackErr{err: err, pcs: callers()}
}

// New creates an error carrying the caller's stack.
func New(msg string) error { return &stackErr{err: errors.New(msg), pcs: callers()} }

// Errorf is fmt.Errorf with a captured stack (supports %w).
func Errorf(format string, a ...any) error {
	return &stackErr{err: fmt.Errorf(format, a...), pcs: callers()}
}

// Frames returns the recorded stack as frames, skipping this package's own
// frames. Empty when err carries no stack.
func Frames(err error) []runtime.Frame {
	var sc stackCarrier
	if !errors.As(err, &sc) {
		return nil
	}
	cf := runtime.CallersFrames(sc.stack())
	var out []runtime.Frame
	for {
		f, more := cf.Next()
		if f.Function != "" && !strings.Contains(f.Function, "github.com/kamu/goapp/errs") {
			out = append(out, f)
		}
		if !more {
			break
		}
	}
	return out
}

// Caller returns "file:line" of the origin frame, or "" when there is no stack.
func Caller(err error) string {
	fr := Frames(err)
	if len(fr) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", shortFile(fr[0].File), fr[0].Line)
}

// Format renders the stack as "func\n\tfile:line" lines.
func Format(err error) string {
	fr := Frames(err)
	if len(fr) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range fr {
		fmt.Fprintf(&b, "%s\n\t%s:%d\n", f.Function, f.File, f.Line)
	}
	return b.String()
}

// shortFile keeps the last two path segments for readability.
func shortFile(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
