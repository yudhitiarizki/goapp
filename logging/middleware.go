package logging

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yudhitiarizki/goapp/reqctx"
)

// maxBodyLog caps how many bytes of a request/response body are logged.
const maxBodyLog = 4 << 10 // 4 KiB

type bodyLogWriter struct {
	gin.ResponseWriter
	buf bytes.Buffer
}

func (w *bodyLogWriter) capture(b []byte) {
	if n := maxBodyLog - w.buf.Len(); n > 0 {
		if len(b) > n {
			b = b[:n]
		}
		w.buf.Write(b)
	}
}

func (w *bodyLogWriter) Write(b []byte) (int, error) {
	w.capture(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyLogWriter) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

// GinLogger emits one type=api record per HTTP request, with body/params/query
// captured. On a 5xx (or any handler error reported via c.Error / response), it
// additionally emits a type=error record so failures are easy to isolate.
func (l *Logger) GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		var reqBody string
		if c.Request.Body != nil && isTextContent(c.ContentType()) {
			buf, _ := io.ReadAll(io.LimitReader(c.Request.Body, maxBodyLog+1))
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), c.Request.Body))
			reqBody = truncate(buf)
		}

		blw := &bodyLogWriter{ResponseWriter: c.Writer}
		c.Writer = blw

		c.Next()

		status := c.Writer.Status()
		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.Int("respHttpStatus", status),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
			slog.String("client_ip", c.ClientIP()),
			slog.Int("bytes", c.Writer.Size()),
			slog.String("user_agent", c.Request.UserAgent()),
		}
		if m := paramsMap(c); len(m) > 0 {
			attrs = append(attrs, slog.Any("request_params", m))
		}
		if q := queryMap(c); len(q) > 0 {
			attrs = append(attrs, slog.Any("request_query", q))
		}
		if reqBody != "" {
			attrs = append(attrs, slog.String("request_body", reqBody))
		}
		if isTextContent(c.Writer.Header().Get("Content-Type")) && blw.buf.Len() > 0 {
			attrs = append(attrs, slog.String("response_body", truncate(blw.buf.Bytes())))
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("error", c.Errors.String()))
		}

		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		l.emit(c.Request.Context(), level, TypeAPI, c.Request.URL.Path, "http_request", attrs)

		// Surface server-side failures as their own error record, using the
		// stack captured at the error's origin (errs.Wrap) when available.
		if status >= 500 {
			var err error
			if len(c.Errors) > 0 {
				err = c.Errors.Last().Err
			}
			msg := "server error"
			if err != nil {
				msg = err.Error()
			}
			caller, stack := stackOf(err)
			errAttrs := []slog.Attr{slog.String("error", msg), slog.String("stack", stack)}
			if caller != "" {
				errAttrs = append(errAttrs, slog.String("error_caller", caller))
			}
			l.emit(c.Request.Context(), slog.LevelError, TypeError, c.Request.URL.Path, msg, errAttrs)
		}
	}
}

// GinRecovery turns a panic into a type=error log with the real panic stack and
// a 500 response, keeping the same request id.
func (l *Logger) GinRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				l.emit(c.Request.Context(), slog.LevelError, TypeError, c.Request.URL.Path,
					fmt.Sprintf("panic: %v", r),
					[]slog.Attr{slog.String("stack", string(debug.Stack()))})
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
						"success":   false,
						"requestId": reqctx.FromContext(c.Request.Context()).RequestID,
						"error":     "internal server error",
					})
				}
			}
		}()
		c.Next()
	}
}

func paramsMap(c *gin.Context) map[string]string {
	if len(c.Params) == 0 {
		return nil
	}
	m := make(map[string]string, len(c.Params))
	for _, p := range c.Params {
		m[p.Key] = p.Value
	}
	return m
}

func queryMap(c *gin.Context) map[string]string {
	q := c.Request.URL.Query()
	if len(q) == 0 {
		return nil
	}
	m := make(map[string]string, len(q))
	for k, vs := range q {
		m[k] = strings.Join(vs, ",")
	}
	return m
}

func isTextContent(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "json") ||
		strings.Contains(ct, "text") ||
		strings.Contains(ct, "x-www-form-urlencoded")
}

func truncate(b []byte) string {
	if len(b) > maxBodyLog {
		return string(b[:maxBodyLog]) + "...(truncated)"
	}
	return string(b)
}
