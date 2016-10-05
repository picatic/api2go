package api2go

import (
	"context"
	"net/http"
	"strings"

	"github.com/manyminds/api2go/jsonapi"
	"github.com/manyminds/api2go/routing"
)

// DefaultContentMarshalers is the default set of content marshalers for an API.
// Currently this means handling application/vnd.api+json content type bodies
// using the standard encoding/json package.
var DefaultContentMarshalers = map[string]ContentMarshaler{
	defaultContentTypeHeader: JSONContentMarshaler{},
}

// API is a REST JSONAPI.
type API struct {
	router      routing.Routeable
	info        Information
	resources   []resource
	marshalers  map[string]ContentMarshaler
	middlewares routing.Chain
	Context     context.Context
}

func (api API) SetRouter(router routing.Routeable) {
	api.router = router
}

// Handler returns the http.Handler instance for the API.
func (api API) Handler() http.Handler {
	return api.middlewares.Handler(api.router.Handler())
}

//Router returns the specified router on an api instance
func (api API) Router() routing.Routeable {
	return api.router
}

// AddResource registers a data source for the given resource
// At least the CRUD interface must be implemented, all the other interfaces are optional.
// `resource` should be either an empty struct instance such as `Post{}` or a pointer to
// a struct such as `&Post{}`. The same type will be used for constructing new elements.
func (api *API) AddResource(prototype jsonapi.MarshalIdentifier, source CRUD) {
	api.addResource(prototype, source, api.marshalers)
}

// UseMiddleware registers middlewares that implement the api2go.HandlerFunc
// Middleware is run before any generated routes.
func (api *API) UseMiddleware(middleware ...func(http.Handler) http.Handler) {
	api.middlewares = append(api.middlewares, middleware...)
}

// SetRedirectTrailingSlash enables 307 redirects on urls ending with /
// when disabled, an URL ending with / will 404
// this will and should work only if using the default router
// DEPRECATED
func (api *API) SetRedirectTrailingSlash(enabled bool) {
	if api.router == nil {
		panic("router must not be nil")
	}

	httpRouter, ok := api.router.(*routing.HTTPRouter)
	if !ok {
		panic("can not set redirectTrailingSlashes if not using the internal httpRouter")
	}

	httpRouter.SetRedirectTrailingSlash(enabled)
}

// NewAPIWithMarshalling does the same as NewAPIWithBaseURL with the addition
// of a set of marshalers that provide a way to interact with clients that
// use a serialization format other than JSON. The marshalers map is indexed
// by the MIME content type to use for a given request-response pair. If the
// client provides an Accept header the server will respond using the client's
// preferred content type, otherwise it will respond using whatever content
// type the client provided in its Content-Type request header.
func NewAPIWithMarshalling(prefix string, resolver URLResolver, marshalers map[string]ContentMarshaler, ctx context.Context) *API {
	r := routing.NewHTTPRouter(prefix, NotAllowedHandler{marshalers: marshalers})
	return newAPI(prefix, resolver, marshalers, r, nil)
}

// NewAPIWithBaseURL does the same as NewAPI with the addition of
// a baseURL which get's added in front of all generated URLs.
// For example http://localhost/v1/myResource/abc instead of /v1/myResource/abc
func NewAPIWithBaseURL(prefix string, baseURL string) *API {
	return NewAPIWithMarshalers(prefix, baseURL, DefaultContentMarshalers)
}

// NewAPI returns an initialized API instance
// `prefix` is added in front of all endpoints.
func NewAPI(prefix string) *API {
	return NewAPIWithMarshalers(prefix, "", DefaultContentMarshalers)
}

// NewAPIwithContext returns an initialized API instance with a context
// `prefix` is added in front of all endpoints.
func NewAPIWithContext(prefix string, ctx context.Context) *API {
	newAPI := NewAPIWithMarshalling(prefix, NewStaticResolver(""), DefaultContentMarshalers, ctx)
	return newAPI
}

// NewAPIWithContextAndRouting returns an initialized API instance with a context and routing
// `prefix` is added in front of all endpoints.
func NewAPIWithContextAndRouting(prefix string, ctx context.Context, router routing.Routeable) *API {
	newAPI := NewAPIWithMarshalers(prefix, "", DefaultContentMarshalers)
	newAPI.Context = ctx
	newAPI.router = router
	return newAPI
}

// NewAPIWithRouting allows you to use a custom URLResolver, marshalers and custom routing
// if you want to use the default routing, you should use another constructor.
//
// If you don't need any of the parameters you can skip them with the defaults:
// the default for `prefix` would be `""`, which means there is no namespace for your api.
// although we suggest using one.
//
// if your api only answers to one url you can use a NewStaticResolver() as  `resolver`
//
// if you have no specific marshalling needs, use `DefaultContentMarshalers`
func NewAPIWithRouting(prefix string, resolver URLResolver, marshalers map[string]ContentMarshaler, router routing.Routeable) *API {
	return newAPI(prefix, resolver, marshalers, router, nil)
}

// newAPI is now an internal method that can be changed if params are changing
func newAPI(prefix string, resolver URLResolver, marshalers map[string]ContentMarshaler, router routing.Routeable, ctx context.Context) *API {
	if len(marshalers) == 0 {
		panic("marshaler map must not be empty")
	}

	// Add initial and trailing slash to prefix
	prefixSlashes := strings.Trim(prefix, "/")
	if len(prefixSlashes) > 0 {
		prefixSlashes = "/" + prefixSlashes + "/"
	} else {
		prefixSlashes = "/"
	}

	info := Information{prefix: prefix, resolver: resolver}

	api := &API{
		router:     router,
		info:       info,
		marshalers: marshalers,
		Context:    ctx,
	}

	requestInfo := func(r *http.Request, api *API) Information {
		var info Information
		if resolver, ok := api.info.resolver.(RequestAwareURLResolver); ok {
			resolver.SetRequest(*r)
			info = Information{prefix: api.info.prefix, resolver: resolver}
		} else {
			info = api.info
		}

		return info
	}

	api.middlewares = append(api.middlewares, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var c context.Context
			if api.Context != nil {
				c = api.Context
			} else {
				c = r.Context()
			}
			c = context.WithValue(c, api_info, requestInfo(r, api))
			c = context.WithValue(c, api_prefix, strings.Trim(api.info.prefix, "/"))
			c = context.WithValue(c, api_api, api)
			next.ServeHTTP(w, r.WithContext(c))
		})
	})
	return api
}

// NewAPIWithMarshalers is DEPRECATED
// use NewApiWithMarshalling instead
func NewAPIWithMarshalers(prefix string, baseURL string, marshalers map[string]ContentMarshaler) *API {
	staticResolver := NewStaticResolver(baseURL)
	return NewAPIWithMarshalling(prefix, staticResolver, marshalers, nil)
}
