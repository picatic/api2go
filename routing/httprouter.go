package routing

import (
	"net/http"

	"github.com/rs/xhandler"
	"github.com/rs/xmux"
	"golang.org/x/net/context"
)

// HTTPRouter default router implementation for api2go
type HTTPRouter struct {
	router *xmux.Mux
}

// Handle each method like before and wrap them into julienschmidt handler func style
func (h HTTPRouter) Handle(protocol, route string, handler HandlerFuncC) {
	handlerFunc := (func(context.Context, http.ResponseWriter, *http.Request))(handler)
	h.router.HandleC(protocol, route, xhandler.HandlerFuncC(handlerFunc))
}

// Handler returns the router
func (h HTTPRouter) Handler() xhandler.HandlerC {
	return h.router
}

func (h HTTPRouter) Param(ctx context.Context, name string) string {
	return xmux.Param(ctx, name)
}

func (h HTTPRouter) SetParam(ctx context.Context, name, value string) {

}

// SetRedirectTrailingSlash wraps this internal functionality of
// the julienschmidt router.
func (h HTTPRouter) SetRedirectTrailingSlash(enabled bool) {
	h.router.RedirectTrailingSlash = enabled
}

// NewHTTPRouter returns a new instance of julienschmidt/httprouter
// this is the default router when using api2go
func NewHTTPRouter(prefix string, notAllowedHandler http.Handler) Routeable {
	router := xmux.New()
	router.HandleMethodNotAllowed = true
	router.MethodNotAllowed = xhandler.HandlerFuncC(func(_ context.Context, w http.ResponseWriter, r *http.Request) {
		notAllowedHandler.ServeHTTP(w, r)
	})
	return &HTTPRouter{router: router}
}
