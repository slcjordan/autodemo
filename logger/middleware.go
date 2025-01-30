package logger

import (
	"net/http"
	"time"
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := WithValue(r.Context(), "method", r.Method)
		ctx = WithValue(ctx, "remote", r.RemoteAddr)
		ctx = WithValue(ctx, "agent", r.UserAgent())
		next.ServeHTTP(w, r.WithContext(ctx))
		ctx = WithValue(ctx, "duration", time.Now().Sub(start))
		Infof(ctx, "%s", r.URL)
	})
}
