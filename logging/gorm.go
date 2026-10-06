package logging

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger returns a gorm.io/gorm/logger.Interface that emits one type=db
// record per query. GORM passes the request context into Trace, so each query
// carries the same request_id as the HTTP request that triggered it. The log's
// path is the calling Go method (e.g. "pasien.(*repository).FindByDokterID").
func (l *Logger) GormLogger() gormlogger.Interface {
	return &gormLogger{l: l}
}

type gormLogger struct {
	l *Logger
}

func (g *gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return g }

func (g *gormLogger) Info(ctx context.Context, msg string, data ...any) {
	g.l.emit(ctx, slog.LevelInfo, TypeDB, "", msg, nil)
}

func (g *gormLogger) Warn(ctx context.Context, msg string, data ...any) {
	g.l.emit(ctx, slog.LevelWarn, TypeDB, "", msg, nil)
}

func (g *gormLogger) Error(ctx context.Context, msg string, data ...any) {
	g.l.emit(ctx, slog.LevelError, TypeDB, "", msg, nil)
}

func (g *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	attrs := []slog.Attr{
		slog.String("sql", sql),
		slog.Int64("rows", rows),
		slog.Int64("latency_ms", elapsed.Milliseconds()),
	}
	level := slog.LevelInfo
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		attrs = append(attrs, slog.String("error", err.Error()))
		level = slog.LevelError
	}
	g.l.emit(ctx, level, TypeDB, callerMethod(), "db_query", attrs)
}
