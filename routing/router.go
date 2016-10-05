package routing

import (
	"net/http"

	"context"
)

type HandlerFuncC func(context.Context, http.ResponseWriter, *http.Request)

// Routeable allows drop in replacement for api2go's router
// by default, we are using julienschmidt/httprouter
// but you can use any router that has similiar features
// e.g. gin
type Routeable interface {
	// Handler should return the routers main handler, often this is the router itself
	Handler() http.Handler
	// Handle must be implemented to register api2go's default routines
	// to your used router.
	// protocol will be PATCH,OPTIONS,GET,POST,PUT
	// route will be the request route /items/:id where :id means dynamically filled params
	// handler is the handler that will answer to this specific route
	Handle(protocol, route string, handler http.HandlerFunc)

	Param(context.Context, string) string
	SetParam(context.Context, string, string)
}
