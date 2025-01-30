package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

type contextKeyType string

var contextKey = contextKeyType("log")

func extract(ctx context.Context) []slog.Attr {
	result, _ := ctx.Value(contextKey).([]slog.Attr)
	return result
}

func WithValue(ctx context.Context, key string, val any) context.Context {
	attrs := extract(ctx)
	switch v := val.(type) {
	case bool:
		attrs = append(attrs, slog.Bool(key, v))
	case time.Duration:
		attrs = append(attrs, slog.Duration(key, v))
	case float64:
		attrs = append(attrs, slog.Float64(key, v))
	case []any:
		attrs = append(attrs, slog.Group(key, v...))
	case int:
		attrs = append(attrs, slog.Int(key, v))
	case int64:
		attrs = append(attrs, slog.Int64(key, v))
	case string:
		attrs = append(attrs, slog.String(key, v))
	case time.Time:
		attrs = append(attrs, slog.Time(key, v))
	case uint64:
		attrs = append(attrs, slog.Uint64(key, v))
	default:
		attrs = append(attrs, slog.Any(key, v))
	}
	return context.WithValue(ctx, contextKey, attrs)
}

var infoLog = slog.New(slog.NewJSONHandler(os.Stdout, nil))
var errorLog = slog.New(slog.NewJSONHandler(os.Stderr, nil))

func Infof(ctx context.Context, f string, args ...any) {
	infoLog.LogAttrs(ctx, slog.LevelInfo, fmt.Sprintf(f, args...), extract(ctx)...)
}

func Errorf(ctx context.Context, f string, args ...any) {
	errorLog.LogAttrs(ctx, slog.LevelError, fmt.Sprintf(f, args...), extract(ctx)...)
}
