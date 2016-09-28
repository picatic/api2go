package api2go

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/gddo/httputil"
	"github.com/manyminds/api2go/jsonapi"
	"github.com/manyminds/api2go/routing"
	"golang.org/x/net/context"
)

const (
	codeInvalidQueryFields   = "API2GO_INVALID_FIELD_QUERY_PARAM"
	defaultContentTypeHeader = "application/vnd.api+json; charset=utf-8"
	api_info                 = "API:INFO"
	api_relation             = "API:RELATION"
	api_linked               = "API:LINKED"
	api_prefix               = "API:PREFIX"
	api_api                  = "API"
	API_ERROR                = "API:ERROR"
)

var queryFieldsRegex = regexp.MustCompile(`^fields\[(\w+)\]$`)

type response struct {
	Meta   map[string]interface{}
	Data   interface{}
	Status int
}

func (r response) Metadata() map[string]interface{} {
	return r.Meta
}

func (r response) Result() interface{} {
	return r.Data
}

func (r response) StatusCode() int {
	return r.Status
}

type Information struct {
	prefix   string
	resolver URLResolver
}

func (i Information) GetBaseURL() string {
	return i.resolver.GetBaseURL()
}

func (i Information) GetPrefix() string {
	return i.prefix
}

type PaginationQueryParams struct {
	number, size, offset, limit string
}

func NewPaginationQueryParams(r *http.Request) PaginationQueryParams {
	var result PaginationQueryParams

	queryParams := r.URL.Query()
	result.number = queryParams.Get("page[number]")
	result.size = queryParams.Get("page[size]")
	result.offset = queryParams.Get("page[offset]")
	result.limit = queryParams.Get("page[limit]")

	return result
}

func (p PaginationQueryParams) IsValid() bool {
	if p.number == "" && p.size == "" && p.offset == "" && p.limit == "" {
		return false
	}

	if p.number != "" && p.size != "" && p.offset == "" && p.limit == "" {
		return true
	}

	if p.number == "" && p.size == "" && p.offset != "" && p.limit != "" {
		return true
	}

	return false
}

func (p PaginationQueryParams) GetLinks(r *http.Request, count uint, info Information) (result map[string]string, err error) {
	result = make(map[string]string)

	params := r.URL.Query()
	prefix := ""
	baseURL := info.GetBaseURL()
	if baseURL != "" {
		prefix = baseURL
	}
	requestURL := fmt.Sprintf("%s%s", prefix, r.URL.Path)

	if p.number != "" {
		// we have number & size params
		var number uint64
		number, err = strconv.ParseUint(p.number, 10, 64)
		if err != nil {
			return
		}

		if p.number != "1" {
			params.Set("page[number]", "1")
			query, _ := url.QueryUnescape(params.Encode())
			result["first"] = fmt.Sprintf("%s?%s", requestURL, query)

			params.Set("page[number]", strconv.FormatUint(number-1, 10))
			query, _ = url.QueryUnescape(params.Encode())
			result["prev"] = fmt.Sprintf("%s?%s", requestURL, query)
		}

		// calculate last page number
		var size uint64
		size, err = strconv.ParseUint(p.size, 10, 64)
		if err != nil {
			return
		}
		totalPages := (uint64(count) / size)
		if (uint64(count) % size) != 0 {
			// there is one more page with some len(items) < size
			totalPages++
		}

		if number != totalPages {
			params.Set("page[number]", strconv.FormatUint(number+1, 10))
			query, _ := url.QueryUnescape(params.Encode())
			result["next"] = fmt.Sprintf("%s?%s", requestURL, query)

			params.Set("page[number]", strconv.FormatUint(totalPages, 10))
			query, _ = url.QueryUnescape(params.Encode())
			result["last"] = fmt.Sprintf("%s?%s", requestURL, query)
		}
	} else {
		// we have offset & limit params
		var offset, limit uint64
		offset, err = strconv.ParseUint(p.offset, 10, 64)
		if err != nil {
			return
		}
		limit, err = strconv.ParseUint(p.limit, 10, 64)
		if err != nil {
			return
		}

		if p.offset != "0" {
			params.Set("page[offset]", "0")
			query, _ := url.QueryUnescape(params.Encode())
			result["first"] = fmt.Sprintf("%s?%s", requestURL, query)

			var prevOffset uint64
			if limit > offset {
				prevOffset = 0
			} else {
				prevOffset = offset - limit
			}
			params.Set("page[offset]", strconv.FormatUint(prevOffset, 10))
			query, _ = url.QueryUnescape(params.Encode())
			result["prev"] = fmt.Sprintf("%s?%s", requestURL, query)
		}

		// check if there are more entries to be loaded
		if (offset + limit) < uint64(count) {
			params.Set("page[offset]", strconv.FormatUint(offset+limit, 10))
			query, _ := url.QueryUnescape(params.Encode())
			result["next"] = fmt.Sprintf("%s?%s", requestURL, query)

			params.Set("page[offset]", strconv.FormatUint(uint64(count)-limit, 10))
			query, _ = url.QueryUnescape(params.Encode())
			result["last"] = fmt.Sprintf("%s?%s", requestURL, query)
		}
	}

	return
}

type NotAllowedHandler struct {
	marshalers map[string]ContentMarshaler
}

func (n notAllowedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := NewHTTPError(nil, "Method Not Allowed", http.StatusMethodNotAllowed)
	w.WriteHeader(http.StatusMethodNotAllowed)
	HandleError(err, w, r, n.marshalers)
}

type resource struct {
	resourceType reflect.Type
	source       CRUD
	name         string
	marshalers   map[string]ContentMarshaler
}

func (api *API) addResource(prototype jsonapi.MarshalIdentifier, source CRUD, marshalers map[string]ContentMarshaler) *resource {
	resourceType := reflect.TypeOf(prototype)
	if resourceType.Kind() != reflect.Struct && resourceType.Kind() != reflect.Ptr {
		panic("pass an empty resource struct or a struct pointer to AddResource!")
	}

	var ptrPrototype interface{}
	var name string

	if resourceType.Kind() == reflect.Struct {
		ptrPrototype = reflect.New(resourceType).Interface()
		name = resourceType.Name()
	} else {
		ptrPrototype = reflect.ValueOf(prototype).Interface()
		name = resourceType.Elem().Name()
	}

	// check if EntityNamer interface is implemented and use that as name
	entityName, ok := prototype.(jsonapi.EntityNamer)
	if ok {
		name = entityName.GetName()
	} else {
		name = jsonapi.Jsonify(jsonapi.Pluralize(name))
	}

	res := resource{
		resourceType: resourceType,
		name:         name,
		source:       source,
		marshalers:   marshalers,
	}

	prefix := strings.Trim(api.info.prefix, "/")
	baseURL := "/" + name
	if prefix != "" {
		baseURL = "/" + prefix + baseURL
	}

	api.router.Handle("OPTIONS", baseURL, func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET,POST,PATCH,OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	})

	api.router.Handle("OPTIONS", baseURL+"/:id", func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET,PATCH,DELETE,OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	})

	api.router.Handle("GET", baseURL, func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		err := res.handleIndex(ctx, w, r)
		if err != nil {
			HandleError(err, w, r, marshalers)
		}
	})

	api.router.Handle("GET", baseURL+"/:id", func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		err := res.handleRead(ctx, w, r, api.router.Param)
		if err != nil {
			HandleError(err, w, r, marshalers)
		}
	})

	// generate all routes for linked relations if there are relations
	casted, ok := prototype.(jsonapi.MarshalReferences)
	if ok {
		relations := casted.GetReferences()
		for _, relation := range relations {
			api.router.Handle("GET", baseURL+"/:id/relationships/"+relation.Name, func(relation jsonapi.Reference) routing.HandlerFuncC {
				return func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
					ctx = context.WithValue(ctx, api_relation, relation.Name)
					err := res.handleReadRelation(ctx, w, r, api.router.Param)
					if err != nil {
						HandleError(err, w, r, marshalers)
					}
				}
			}(relation))

			api.router.Handle("GET", baseURL+"/:id/"+relation.Name, func(relation jsonapi.Reference) routing.HandlerFuncC {
				return func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
					ctx = context.WithValue(ctx, api_linked, relation)
					err := res.handleLinked(ctx, api, w, r, api.router.Param)
					if err != nil {
						HandleError(err, w, r, marshalers)
					}
				}
			}(relation))

			api.router.Handle("PATCH", baseURL+"/:id/relationships/"+relation.Name, func(relation jsonapi.Reference) routing.HandlerFuncC {
				return func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
					ctx = context.WithValue(ctx, api_relation, relation.Name)
					err := res.handleReplaceRelation(ctx, w, r, api.router.Param)
					if err != nil {
						HandleError(err, w, r, marshalers)
					}
				}
			}(relation))

			if _, ok := ptrPrototype.(jsonapi.EditToManyRelations); ok && relation.Name == jsonapi.Pluralize(relation.Name) {
				// generate additional routes to manipulate to-many relationships
				api.router.Handle("POST", baseURL+"/:id/relationships/"+relation.Name, func(relation jsonapi.Reference) routing.HandlerFuncC {
					return func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
						ctx = context.WithValue(ctx, api_relation, relation.Name)
						err := res.handleAddToManyRelation(ctx, w, r, api.router.Param)
						if err != nil {
							HandleError(err, w, r, marshalers)
						}
					}
				}(relation))

				api.router.Handle("DELETE", baseURL+"/:id/relationships/"+relation.Name, func(relation jsonapi.Reference) routing.HandlerFuncC {
					return func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
						ctx = context.WithValue(ctx, api_relation, relation.Name)
						err := res.handleDeleteToManyRelation(ctx, w, r, api.router.Param)
						if err != nil {
							HandleError(err, w, r, marshalers)
						}
					}
				}(relation))
			}
		}
	}

	api.router.Handle("POST", baseURL, func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		err := res.handleCreate(ctx, w, r)
		if err != nil {
			HandleError(err, w, r, marshalers)
		}
	})

	api.router.Handle("DELETE", baseURL+"/:id", func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		err := res.handleDelete(ctx, w, r, api.router.Param)
		if err != nil {
			HandleError(err, w, r, marshalers)
		}
	})

	api.router.Handle("PATCH", baseURL+"/:id", func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		err := res.handleUpdate(ctx, w, r, api.router.Param)
		if err != nil {
			HandleError(err, w, r, marshalers)
		}
	})

	api.resources = append(api.resources, res)

	return &res
}

func BuildRequest(c context.Context, r *http.Request) Request {
	req := Request{PlainRequest: r}
	params := make(map[string][]string)
	for key, values := range r.URL.Query() {
		params[key] = strings.Split(values[0], ",")
	}
	req.QueryParams = params
	req.Header = r.Header
	req.Context = c
	return req
}

func (res *resource) handleIndex(c context.Context, w http.ResponseWriter, r *http.Request) error {
	info := c.Value(api_info).(Information)

	pagination := NewPaginationQueryParams(r)
	if pagination.IsValid() {
		source, ok := res.source.(PaginatedFindAll)
		if !ok {
			return NewHTTPError(nil, "Resource does not implement the PaginatedFindAll interface", http.StatusNotFound)
		}

		count, response, err := source.PaginatedFindAll(BuildRequest(c, r))
		if err != nil {
			return err
		}

		paginationLinks, err := pagination.GetLinks(r, count, info)
		if err != nil {
			return err
		}

		return RespondWithPagination(response, info, http.StatusOK, paginationLinks, w, r, res.marshalers)
	}
	source, ok := res.source.(FindAll)
	if !ok {
		return NewHTTPError(nil, "Resource does not implement the FindAll interface", http.StatusNotFound)
	}

	response, err := source.FindAll(BuildRequest(c, r))
	if err != nil {
		return err
	}

	return RespondWith(response, http.StatusOK, c, w, r)
}

func (res *resource) handleRead(c context.Context, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	id := params(c, "id")

	response, err := res.source.FindOne(id, BuildRequest(c, r))

	if err != nil {
		return err
	}

	return RespondWith(response, http.StatusOK, c, w, r)
}

func (res *resource) handleReadRelation(c context.Context, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	id := params(c, "id")

	obj, err := res.source.FindOne(id, BuildRequest(c, r))
	if err != nil {
		return err
	}

	internalError := NewHTTPError(nil, "Internal server error, invalid object structure", http.StatusInternalServerError)

	info := c.Value(api_info).(Information)
	marshalled, err := jsonapi.MarshalWithURLs(obj.Result(), info)
	data, ok := marshalled["data"]
	if !ok {
		return internalError
	}
	relationships, ok := data.(map[string]interface{})["relationships"]
	if !ok {
		return internalError
	}

	relName := c.Value(api_relation).(string)
	rel, ok := relationships.(map[string]map[string]interface{})[relName]
	if !ok {
		return NewHTTPError(nil, fmt.Sprintf("There is no relation with the name %s", relName), http.StatusNotFound)
	}
	links, ok := rel["links"].(map[string]string)
	if !ok {
		return internalError
	}
	self, ok := links["self"]
	if !ok {
		return internalError
	}
	related, ok := links["related"]
	if !ok {
		return internalError
	}
	relationData, ok := rel["data"]
	if !ok {
		return internalError
	}

	result := map[string]interface{}{}
	result["links"] = map[string]interface{}{
		"self":    self,
		"related": related,
	}
	result["data"] = relationData
	meta := obj.Metadata()
	if len(meta) > 0 {
		result["meta"] = meta
	}

	return marshalResponse(result, w, http.StatusOK, r, res.marshalers)
}

// try to find the referenced resource and call the findAll Method with referencing resource id as param
func (res *resource) handleLinked(c context.Context, api *API, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	id := params(c, "id")
	info := c.Value(api_info).(Information)
	linked := c.Value(api_linked).(jsonapi.Reference)
	for _, resource := range api.resources {
		if resource.name == linked.Type {
			request := BuildRequest(c, r)
			request.QueryParams[res.name+"ID"] = []string{id}
			request.QueryParams[res.name+"Name"] = []string{linked.Name}

			// check for pagination, otherwise normal FindAll
			pagination := NewPaginationQueryParams(r)
			if pagination.IsValid() {
				source, ok := resource.source.(PaginatedFindAll)
				if !ok {
					return NewHTTPError(nil, "Resource does not implement the PaginatedFindAll interface", http.StatusNotFound)
				}

				var count uint
				count, response, err := source.PaginatedFindAll(request)
				if err != nil {
					return err
				}

				paginationLinks, err := pagination.GetLinks(r, count, info)
				if err != nil {
					return err
				}

				return RespondWithPagination(response, info, http.StatusOK, paginationLinks, w, r, res.marshalers)
			}

			source, ok := resource.source.(FindAll)
			if !ok {
				return NewHTTPError(nil, "Resource does not implement the FindAll interface", http.StatusNotFound)
			}

			obj, err := source.FindAll(request)
			if err != nil {
				return err
			}
			return RespondWith(obj, http.StatusOK, c, w, r)
		}
	}

	err := Error{
		Status: string(http.StatusNotFound),
		Title:  "Not Found",
		Detail: "No resource handler is registered to handle the linked resource " + linked.Name,
	}

	answ := response{Data: err, Status: http.StatusNotFound}

	return RespondWith(answ, http.StatusNotFound, c, w, r)

}

func (res *resource) handleCreate(c context.Context, w http.ResponseWriter, r *http.Request) error {
	ctx, err := unmarshalRequest(r, res.marshalers)
	prefix := c.Value(api_prefix).(string)
	if err != nil {
		return err
	}
	newObjs := reflect.MakeSlice(reflect.SliceOf(res.resourceType), 0, 0)

	structType := res.resourceType
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	err = jsonapi.UnmarshalInto(ctx, structType, &newObjs)
	if err != nil {
		return err
	}
	if newObjs.Len() != 1 {
		return errors.New("expected one object in POST")
	}

	//TODO create multiple objects not only one.
	newObj := newObjs.Index(0).Interface()

	response, err := res.source.Create(newObj, BuildRequest(c, r))
	if err != nil {
		return err
	}

	result, ok := response.Result().(jsonapi.MarshalIdentifier)

	if !ok {
		return fmt.Errorf("Expected one newly created object by resource %s", res.name)
	}

	w.Header().Set("Location", "/"+prefix+"/"+res.name+"/"+result.GetID())

	// handle 200 status codes
	switch response.StatusCode() {
	case http.StatusCreated:
		return RespondWith(response, http.StatusCreated, c, w, r)
	case http.StatusNoContent:
		w.WriteHeader(response.StatusCode())
		return nil
	case http.StatusAccepted:
		w.WriteHeader(response.StatusCode())
		return nil
	default:
		return fmt.Errorf("invalid status code %d from resource %s for method Create", response.StatusCode(), res.name)
	}
}

func (res *resource) handleUpdate(c context.Context, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	id := params(c, "id")
	obj, err := res.source.FindOne(id, BuildRequest(c, r))
	if err != nil {
		return err
	}

	ctx, err := unmarshalRequest(r, res.marshalers)
	if err != nil {
		return err
	}

	data, ok := ctx["data"]

	if !ok {
		return NewHTTPError(
			errors.New("Forbidden"),
			"missing mandatory data key.",
			http.StatusForbidden,
		)
	}

	check, ok := data.(map[string]interface{})
	if !ok {
		return NewHTTPError(
			errors.New("Forbidden"),
			"data must contain an object.",
			http.StatusForbidden,
		)
	}

	if _, ok := check["id"]; !ok {
		return NewHTTPError(
			errors.New("Forbidden"),
			"missing mandatory id key.",
			http.StatusForbidden,
		)
	}

	if _, ok := check["type"]; !ok {
		return NewHTTPError(
			errors.New("Forbidden"),
			"missing mandatory type key.",
			http.StatusForbidden,
		)
	}

	updatingObjs := reflect.MakeSlice(reflect.SliceOf(res.resourceType), 1, 1)
	updatingObjs.Index(0).Set(reflect.ValueOf(obj.Result()))

	structType := res.resourceType
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	err = jsonapi.UnmarshalInto(ctx, structType, &updatingObjs)

	if err != nil {
		return err
	}
	if updatingObjs.Len() != 1 {
		return errors.New("expected one object")
	}

	updatingObj := updatingObjs.Index(0).Interface()

	response, err := res.source.Update(updatingObj, BuildRequest(c, r))

	if err != nil {
		return err
	}

	switch response.StatusCode() {
	case http.StatusOK:
		updated := response.Result()
		if updated == nil {
			internalResponse, err := res.source.FindOne(id, BuildRequest(c, r))
			if err != nil {
				return err
			}
			updated = internalResponse.Result()
			if updated == nil {
				return fmt.Errorf("Expected FindOne to return one object of resource %s", res.name)
			}

			response = internalResponse
		}
		return RespondWith(response, http.StatusOK, c, w, r)
	case http.StatusAccepted:
		w.WriteHeader(http.StatusAccepted)
		return nil
	case http.StatusNoContent:
		w.WriteHeader(http.StatusNoContent)
		return nil
	default:
		return fmt.Errorf("invalid status code %d from resource %s for method Update", response.StatusCode(), res.name)
	}
}

func (res *resource) handleReplaceRelation(c context.Context, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	var (
		err     error
		editObj interface{}
	)

	id := params(c, "id")

	response, err := res.source.FindOne(id, BuildRequest(c, r))
	if err != nil {
		return err
	}

	inc, err := unmarshalRequest(r, res.marshalers)
	if err != nil {
		return err
	}

	data, ok := inc["data"]
	if !ok {
		return errors.New("Invalid object. Need a \"data\" object")
	}

	resType := reflect.TypeOf(response.Result()).Kind()
	if resType == reflect.Struct {
		editObj = getPointerToStruct(response.Result())
	} else {
		editObj = response.Result()
	}

	relName := c.Value(api_relation).(string)
	err = jsonapi.UnmarshalRelationshipsData(editObj, relName, data)
	if err != nil {
		return err
	}

	if resType == reflect.Struct {
		_, err = res.source.Update(reflect.ValueOf(editObj).Elem().Interface(), BuildRequest(c, r))
	} else {
		_, err = res.source.Update(editObj, BuildRequest(c, r))
	}

	w.WriteHeader(http.StatusNoContent)
	return err
}

func (res *resource) handleAddToManyRelation(c context.Context, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	var (
		err     error
		editObj interface{}
	)

	id := params(c, "id")
	relName := c.Value(api_relation).(string)

	response, err := res.source.FindOne(id, BuildRequest(c, r))
	if err != nil {
		return err
	}

	inc, err := unmarshalRequest(r, res.marshalers)
	if err != nil {
		return err
	}

	data, ok := inc["data"]
	if !ok {
		return errors.New("Invalid object. Need a \"data\" object")
	}

	newRels, ok := data.([]interface{})
	if !ok {
		return fmt.Errorf("Data must be an array with \"id\" and \"type\" field to add new to-many relationships")
	}

	newIDs := []string{}

	for _, newRel := range newRels {
		casted, ok := newRel.(map[string]interface{})
		if !ok {
			return errors.New("entry in data object invalid")
		}
		newID, ok := casted["id"].(string)
		if !ok {
			return errors.New("no id field found inside data object")
		}

		newIDs = append(newIDs, newID)
	}

	resType := reflect.TypeOf(response.Result()).Kind()
	if resType == reflect.Struct {
		editObj = getPointerToStruct(response.Result())
	} else {
		editObj = response.Result()
	}

	targetObj, ok := editObj.(jsonapi.EditToManyRelations)
	if !ok {
		return errors.New("target struct must implement jsonapi.EditToManyRelations")
	}
	targetObj.AddToManyIDs(relName, newIDs)

	if resType == reflect.Struct {
		_, err = res.source.Update(reflect.ValueOf(targetObj).Elem().Interface(), BuildRequest(c, r))
	} else {
		_, err = res.source.Update(targetObj, BuildRequest(c, r))
	}

	w.WriteHeader(http.StatusNoContent)

	return err
}

func (res *resource) handleDeleteToManyRelation(c context.Context, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	var (
		err     error
		editObj interface{}
	)

	id := params(c, "id")
	relName := c.Value(api_relation).(string)

	response, err := res.source.FindOne(id, BuildRequest(c, r))
	if err != nil {
		return err
	}

	inc, err := unmarshalRequest(r, res.marshalers)
	if err != nil {
		return err
	}

	data, ok := inc["data"]
	if !ok {
		return errors.New("Invalid object. Need a \"data\" object")
	}

	newRels, ok := data.([]interface{})
	if !ok {
		return fmt.Errorf("Data must be an array with \"id\" and \"type\" field to add new to-many relationships")
	}

	obsoleteIDs := []string{}

	for _, newRel := range newRels {
		casted, ok := newRel.(map[string]interface{})
		if !ok {
			return errors.New("entry in data object invalid")
		}
		obsoleteID, ok := casted["id"].(string)
		if !ok {
			return errors.New("no id field found inside data object")
		}

		obsoleteIDs = append(obsoleteIDs, obsoleteID)
	}

	resType := reflect.TypeOf(response.Result()).Kind()
	if resType == reflect.Struct {
		editObj = getPointerToStruct(response.Result())
	} else {
		editObj = response.Result()
	}

	targetObj, ok := editObj.(jsonapi.EditToManyRelations)
	if !ok {
		return errors.New("target struct must implement jsonapi.EditToManyRelations")
	}
	targetObj.DeleteToManyIDs(relName, obsoleteIDs)

	if resType == reflect.Struct {
		_, err = res.source.Update(reflect.ValueOf(targetObj).Elem().Interface(), BuildRequest(c, r))
	} else {
		_, err = res.source.Update(targetObj, BuildRequest(c, r))
	}

	w.WriteHeader(http.StatusNoContent)

	return err
}

// returns a pointer to an interface{} struct
func getPointerToStruct(oldObj interface{}) interface{} {
	resType := reflect.TypeOf(oldObj)
	ptr := reflect.New(resType)
	ptr.Elem().Set(reflect.ValueOf(oldObj))
	return ptr.Interface()
}

func (res *resource) handleDelete(c context.Context, w http.ResponseWriter, r *http.Request, params func(context.Context, string) string) error {
	id := params(c, "id")
	response, err := res.source.Delete(id, BuildRequest(c, r))
	if err != nil {
		return err
	}

	switch response.StatusCode() {
	case http.StatusOK:
		data := map[string]interface{}{
			"meta": response.Metadata(),
		}

		return marshalResponse(data, w, http.StatusOK, r, res.marshalers)
	case http.StatusAccepted:
		w.WriteHeader(http.StatusAccepted)
		return nil
	case http.StatusNoContent:
		w.WriteHeader(http.StatusNoContent)
		return nil
	default:
		return fmt.Errorf("invalid status code %d from resource %s for method Delete", response.StatusCode(), res.name)
	}
}

func writeResult(w http.ResponseWriter, data []byte, status int, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	w.Write(data)
}

func RespondWith(obj Responder, status int, c context.Context, w http.ResponseWriter, r *http.Request) error {
	marshalers := c.Value(api_api).(*API).marshalers
	info := c.Value(api_info).(Information)
	data, err := jsonapi.MarshalWithURLs(obj.Result(), info)
	if err != nil {
		return err
	}

	meta := obj.Metadata()
	if len(meta) > 0 {
		data["meta"] = meta
	}

	return marshalResponse(data, w, status, r, marshalers)
}

func RespondWithPagination(obj Responder, info Information, status int, links map[string]string, w http.ResponseWriter, r *http.Request, marshalers map[string]ContentMarshaler) error {
	data, err := jsonapi.MarshalWithURLs(obj.Result(), info)
	if err != nil {
		return err
	}

	data["links"] = links
	meta := obj.Metadata()
	if len(meta) > 0 {
		data["meta"] = meta
	}

	return marshalResponse(data, w, status, r, marshalers)
}

func unmarshalRequest(r *http.Request, marshalers map[string]ContentMarshaler) (map[string]interface{}, error) {
	defer r.Body.Close()
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{}
	marshaler, _ := selectContentMarshaler(r, marshalers)
	err = marshaler.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func marshalResponse(resp interface{}, w http.ResponseWriter, status int, r *http.Request, marshalers map[string]ContentMarshaler) error {
	marshaler, contentType := selectContentMarshaler(r, marshalers)
	filtered, err := filterSparseFields(resp, r)
	if err != nil {
		return err
	}
	result, err := marshaler.Marshal(filtered)
	if err != nil {
		return err
	}
	writeResult(w, result, status, contentType)
	return nil
}

func filterSparseFields(resp interface{}, r *http.Request) (interface{}, error) {
	query := r.URL.Query()
	queryParams := parseQueryFields(&query)
	if len(queryParams) < 1 {
		return resp, nil
	}

	if content, ok := resp.(map[string]interface{}); ok {
		wrongFields := map[string][]string{}

		// single entry in data
		if data, ok := content["data"].(map[string]interface{}); ok {
			errors := replaceAttributes(&queryParams, &data)
			for t, v := range errors {
				wrongFields[t] = v
			}
		}

		// data can be a slice too
		if datas, ok := content["data"].([]map[string]interface{}); ok {
			for index, data := range datas {
				errors := replaceAttributes(&queryParams, &data)
				for t, v := range errors {
					wrongFields[t] = v
				}
				datas[index] = data
			}
		}

		// included slice
		if included, ok := content["included"].([]map[string]interface{}); ok {
			for index, include := range included {
				errors := replaceAttributes(&queryParams, &include)
				for t, v := range errors {
					wrongFields[t] = v
				}
				included[index] = include
			}
		}

		if len(wrongFields) > 0 {
			httpError := NewHTTPError(nil, "Some requested fields were invalid", http.StatusBadRequest)
			for k, v := range wrongFields {
				for _, field := range v {
					httpError.Errors = append(httpError.Errors, Error{
						Status: "Bad Request",
						Code:   codeInvalidQueryFields,
						Title:  fmt.Sprintf(`Field "%s" does not exist for type "%s"`, field, k),
						Detail: "Please make sure you do only request existing fields",
						Source: &ErrorSource{
							Parameter: fmt.Sprintf("fields[%s]", k),
						},
					})
				}
			}
			return nil, httpError
		}
	}
	return resp, nil
}

func parseQueryFields(query *url.Values) (result map[string][]string) {
	result = map[string][]string{}
	for name, param := range *query {
		matches := queryFieldsRegex.FindStringSubmatch(name)
		if len(matches) > 1 {
			match := matches[1]
			result[match] = strings.Split(param[0], ",")
		}
	}

	return
}

func filterAttributes(attributes map[string]interface{}, fields []string) (filteredAttributes map[string]interface{}, wrongFields []string) {
	wrongFields = []string{}
	filteredAttributes = map[string]interface{}{}

	for _, field := range fields {
		if attribute, ok := attributes[field]; ok {
			filteredAttributes[field] = attribute
		} else {
			wrongFields = append(wrongFields, field)
		}
	}

	return
}

func replaceAttributes(query *map[string][]string, entry *map[string]interface{}) map[string][]string {
	fieldType := (*entry)["type"].(string)
	fields := (*query)[fieldType]
	if len(fields) > 0 {
		if attributes, ok := (*entry)["attributes"]; ok {
			var wrongFields []string
			(*entry)["attributes"], wrongFields = filterAttributes(attributes.(map[string]interface{}), fields)
			if len(wrongFields) > 0 {
				return map[string][]string{
					fieldType: wrongFields,
				}
			}
		}
	}

	return nil
}

func selectContentMarshaler(r *http.Request, marshalers map[string]ContentMarshaler) (marshaler ContentMarshaler, contentType string) {
	if _, found := r.Header["Accept"]; found {
		var contentTypes []string
		for ct := range marshalers {
			contentTypes = append(contentTypes, ct)
		}

		contentType = httputil.NegotiateContentType(r, contentTypes, defaultContentTypeHeader)
		marshaler = marshalers[contentType]
	} else if contentTypes, found := r.Header["Content-Type"]; found {
		contentType = contentTypes[0]
		marshaler = marshalers[contentType]
	}

	if marshaler == nil {
		contentType = defaultContentTypeHeader
		marshaler = JSONContentMarshaler{}
	}

	return
}

func HandleError(err error, w http.ResponseWriter, r *http.Request, marshalers map[string]ContentMarshaler) {
	marshaler, contentType := selectContentMarshaler(r, marshalers)

	log.Println(err)
	if e, ok := err.(HTTPError); ok {
		writeResult(w, []byte(marshaler.MarshalError(err)), e.status, contentType)
		return

	}

	writeResult(w, []byte(marshaler.MarshalError(err)), http.StatusInternalServerError, contentType)
}
