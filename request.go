package api2go

import (
	"net/http"

	"golang.org/x/net/context"
)

// Request contains additional information for FindOne and Find Requests
type Request struct {
	PlainRequest *http.Request
	QueryParams  map[string][]string
	Header       http.Header
	Context      context.Context
}
