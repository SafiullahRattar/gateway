package middleware

import "net/http"

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares so the first in the slice is the outermost wrapper.
// Requests flow left-to-right through the chain before reaching the handler.
func Chain(handler http.Handler, mws ...Middleware) http.Handler {
	// Apply in reverse so the first middleware listed runs first.
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}
	return handler
}
