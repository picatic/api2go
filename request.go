package api2go

import (
	"context"
	"net/http"
)

// Request contains additional information for FindOne and Find Requests
type Request struct {
	PlainRequest *http.Request
	QueryParams  map[string][]string
	Header       http.Header
	Context      context.Context
}
