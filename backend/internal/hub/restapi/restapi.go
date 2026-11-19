// Package restapi describes the /api/v1 routes with Huma, which produces the
// OpenAPI spec. Gin still routes the requests and runs the middleware: every
// operation becomes a Gin handler through Handler, and handlers.go mounts it
// like any other. Huma parses and validates the input, writes the response and
// documents the operation. Its defaults are changed to behave like the Gin
// handlers did, so the frontend and API clients don't notice the difference.
package restapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

const (
	basePath = "/api/v1"

	apiTokenScheme = "apiToken"
	sessionScheme  = "session"
)

// Security requirements for huma.Operation.Security. Routes without one need
// no authentication.
var (
	// Authenticated routes take an API token or the session cookie.
	Authenticated = []map[string][]string{{apiTokenScheme: {}}, {sessionScheme: {}}}
	// SessionOnly routes are for the web UI and refuse API tokens.
	SessionOnly = []map[string][]string{{sessionScheme: {}}}
)

var (
	// registerMu guards the adapter while an operation is registered
	registerMu sync.Mutex
	ginAdapter = &adapter{}
	api        = huma.NewAPI(newConfig(), ginAdapter)
)

func newConfig() huma.Config {
	config := huma.DefaultConfig("OrcaCD API", version.Version)
	config.Info.Description = "The REST API of the OrcaCD hub. Scripts and CI pipelines authenticate with an API token, " +
		"which can only do what its scopes and its owner allow."
	config.Servers = []*huma.Server{{URL: basePath}}
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		apiTokenScheme: {
			Type:        "http",
			Scheme:      "bearer",
			Description: "An API token from Settings → API Tokens, sent as `Authorization: Bearer orca_...`.",
		},
		sessionScheme: {
			Type:        "apiKey",
			In:          "cookie",
			Name:        auth.AuthCookieName,
			Description: "The session of the web UI.",
		},
	}

	// The spec is only published in the docs, the hub doesn't serve it.
	config.OpenAPIPath = ""
	config.DocsPath = ""
	config.SchemasPath = ""
	// Keeps the default hook from adding "$schema" to every response body and a
	// Link header pointing to the schemas, which aren't served.
	config.CreateHooks = nil

	// Gin's binding ignores unknown fields and only requires fields marked with
	// binding:"required". Huma requires every field without omitempty and
	// rejects unknown ones, which would break requests that work today.
	// This also makes response fields optional in the spec, so fields a
	// response always has need required:"true".
	config.AllowAdditionalPropertiesByDefault = true
	config.FieldsOptionalByDefault = true

	config.OnAddOperation = append(config.OnAddOperation, documentValidationErrorsAs400)
	return config
}

// OpenAPI returns the spec of every operation turned into a handler.
func OpenAPI() *huma.OpenAPI {
	return api.OpenAPI()
}

// Handler adds the operation to the spec and returns it as a Gin handler. The
// Gin route must use the operation's method and path, with ":id" for "{id}".
func Handler[I, O any](op huma.Operation, handler func(context.Context, *I) (*O, error)) gin.HandlerFunc {
	registerMu.Lock()
	defer registerMu.Unlock()

	huma.Register(api, op, handler)
	h := ginAdapter.handler
	ginAdapter.handler = nil
	return h
}

type ginContextKey struct{}

// GinContext returns the Gin context of the request a handler runs for, which
// the auth, audit log and security log helpers take.
func GinContext(ctx context.Context) *gin.Context {
	c, ok := ctx.Value(ginContextKey{}).(*gin.Context)
	if !ok {
		panic("restapi: no Gin context, the handler must run through restapi.Handler")
	}
	return c
}

// adapter hands the handler Huma builds for an operation to Handler instead of
// routing requests itself.
type adapter struct {
	handler gin.HandlerFunc
}

func (a *adapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	a.handler = func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ginContextKey{}, c))
		handler(humagin.NewContext(op, c))
	}
}

// ServeHTTP is never called, Gin routes the requests.
func (a *adapter) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

// MessageOutput is the response of actions that return nothing but a message,
// like deleting a resource.
type MessageOutput struct {
	Body struct {
		Message string `json:"message" required:"true" example:"agent deleted"`
	}
}

func Message(message string) *MessageOutput {
	out := &MessageOutput{}
	out.Body.Message = message
	return out
}

// Error is the error body of every route, {"error": "..."}, which the frontend
// reads. Huma's default is an RFC 9457 problem with "title" and "detail".
type Error struct {
	status  int
	Message string `json:"error" doc:"What went wrong" example:"application not found"`
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) GetStatus() int {
	return e.status
}

func init() {
	huma.NewError = newError
}

// documentValidationErrorsAs400 replaces the 422 response Huma documents for
// invalid requests, as newError answers them with 400.
func documentValidationErrorsAs400(_ *huma.OpenAPI, op *huma.Operation) {
	response, ok := op.Responses[strconv.Itoa(http.StatusUnprocessableEntity)]
	if !ok {
		return
	}
	delete(op.Responses, strconv.Itoa(http.StatusUnprocessableEntity))
	badRequest := strconv.Itoa(http.StatusBadRequest)
	if _, ok := op.Responses[badRequest]; !ok {
		response.Description = http.StatusText(http.StatusBadRequest)
		op.Responses[badRequest] = response
	}
}

func newError(status int, msg string, errs ...error) huma.StatusError {
	// Gin answers invalid requests with 400, Huma with 422
	if status == http.StatusUnprocessableEntity {
		status = http.StatusBadRequest
	}

	details := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		var detailer huma.ErrorDetailer
		if errors.As(err, &detailer) {
			// Leaves out the value of the field, which can be a secret
			detail := detailer.ErrorDetail()
			if detail.Location != "" {
				details = append(details, detail.Message+" ("+detail.Location+")")
				continue
			}
			details = append(details, detail.Message)
			continue
		}
		details = append(details, err.Error())
	}
	switch {
	case len(details) == 0:
	case msg == "":
		msg = strings.Join(details, ", ")
	default:
		msg += ": " + strings.Join(details, ", ")
	}

	return &Error{status: status, Message: msg}
}
