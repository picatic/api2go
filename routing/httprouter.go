package routing

import (
	"context"
	"net/http"

	"github.com/dimfeld/httptreemux"
)

// HTTPRouter default router implementation for api2go
type HTTPRouter struct {
	router *httptreemux.TreeMux
	group  *httptreemux.ContextGroup
}

// Handle each method like before and wrap them into julienschmidt handler func style
func (h HTTPRouter) Handle(protocol, route string, handler http.HandlerFunc) {
	h.group.Handle(protocol, route, handler)
}

// Handler returns the router
func (h HTTPRouter) Handler() http.Handler {
	return h.router
}

func (h HTTPRouter) Param(ctx context.Context, name string) string {
	params := httptreemux.ContextParams(ctx)
	return params[name]
}

func (h HTTPRouter) SetParam(ctx context.Context, name, value string) {

}

// SetRedirectTrailingSlash wraps this internal functionality of
// the julienschmidt router.
func (h HTTPRouter) SetRedirectTrailingSlash(enabled bool) {
	h.router.RedirectTrailingSlash = enabled
	h.router.RedirectBehavior = httptreemux.Redirect307
}

// NewHTTPRouter returns a new instance of julienschmidt/httprouter
// this is the default router when using api2go
func NewHTTPRouter(prefix string, notAllowedHandler http.Handler) Routeable {
	router := httptreemux.New()
	router.MethodNotAllowedHandler = func(w http.ResponseWriter, r *http.Request, methods map[string]httptreemux.HandlerFunc) {
		notAllowedHandler.ServeHTTP(w, r)
	}
	group := router.UsingContext()
	return &HTTPRouter{router: router, group: group}
}
