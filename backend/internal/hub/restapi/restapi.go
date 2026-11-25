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
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

const (
	basePath = "/api/v1"
	// Same as the ReadTimeout of the hub's server
	bodyReadTimeout = 10 * time.Second

	apiTokenScheme = "apiToken"
	sessionScheme  = "session"
)

// Security requirements for huma.Operation.Security.
var (
	// Public routes need no authentication.
	Public = []map[string][]string{{}}
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
	config.Info.License = &huma.License{Name: "MIT", URL: "https://github.com/OrcaCD/orca-cd/blob/main/LICENSE"}
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

	// JSON only, even if an import registers more formats in huma.DefaultFormats
	config.Formats = map[string]huma.Format{"application/json": huma.DefaultJSONFormat}

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

	config.OnAddOperation = append(config.OnAddOperation, documentValidationErrorsAs400, documentAuthErrors)
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

	// The hub's server limits the body size for every route, so Huma's stricter
	// default of 1 MiB stays off unless an operation sets its own.
	if op.MaxBodyBytes == 0 {
		op.MaxBodyBytes = -1
	}
	// Huma replaces the server's read deadline while it reads the body, and a
	// negative timeout would remove it. This matches the server's ReadTimeout.
	if op.BodyReadTimeout == 0 {
		op.BodyReadTimeout = bodyReadTimeout
	}

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
		decodeUnlistedBodiesAsJSON(op, c.Request)
		handler(humagin.NewContext(op, c))
		// Huma only sets the status of responses without a body. Writing it here
		// sends it right away, also when a test calls the handler without Gin.
		c.Writer.WriteHeaderNow()
	}
}

// decodeUnlistedBodiesAsJSON makes Huma decode a body as JSON when the request
// sends a content type the operation doesn't list. Gin's ShouldBindJSON read
// JSON whatever the content type, and e.g. `curl -d` sends
// application/x-www-form-urlencoded.
func decodeUnlistedBodiesAsJSON(op *huma.Operation, r *http.Request) {
	if op.RequestBody == nil || op.RequestBody.Content["application/json"] == nil {
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if _, listed := op.RequestBody.Content[mediaType]; listed || mediaType == "" {
		return
	}
	r.Header.Set("Content-Type", "application/json")
}

// ServeHTTP is never called, Gin routes the requests.
func (a *adapter) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

// MessageOutput is the response of actions that return nothing but a message,
// like deleting a resource.
type MessageOutput struct {
	Body struct {
		Message string `json:"message" required:"true"`
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

// validationFailed is the message Huma creates the 422 for invalid requests with
const validationFailed = "validation failed"

// maxErrorDetails caps the details of an error message. Huma reports every
// invalid item, e.g. of a large array, which would make the response huge.
const maxErrorDetails = 5

// documentValidationErrorsAs400 documents the 422 Huma adds for invalid requests
// as 400, as newError answers them with 400. A 422 the operation lists in its
// Errors itself stays.
func documentValidationErrorsAs400(_ *huma.OpenAPI, op *huma.Operation) {
	unprocessable := strconv.Itoa(http.StatusUnprocessableEntity)
	response, ok := op.Responses[unprocessable]
	if !ok {
		return
	}

	badRequest := strconv.Itoa(http.StatusBadRequest)
	if _, ok := op.Responses[badRequest]; !ok {
		op.Responses[badRequest] = &huma.Response{
			Description: http.StatusText(http.StatusBadRequest),
			Content:     response.Content,
		}
	}
	// Huma adds the 422 to op.Errors once for operations with parameters or a
	// body, so a second one comes from the operation.
	added := 0
	if len(op.Parameters) > 0 || op.RequestBody != nil {
		added = 1
	}
	if countOf(op.Errors, http.StatusUnprocessableEntity) <= added {
		delete(op.Responses, unprocessable)
	}
}

// documentAuthErrors adds the errors RequireAuth and RequireSession answer with
// to the operations behind them.
func documentAuthErrors(oapi *huma.OpenAPI, op *huma.Operation) {
	if len(op.Security) == 0 || reflect.DeepEqual(op.Security, Public) {
		return
	}
	authErrors := map[int]string{http.StatusUnauthorized: "Missing, invalid or expired authentication"}
	if reflect.DeepEqual(op.Security, SessionOnly) {
		authErrors[http.StatusForbidden] = "Forbidden, also for requests with an API token"
	}

	schema := oapi.Components.Schemas.Schema(reflect.TypeFor[Error](), true, "Error")
	for status, description := range authErrors {
		if _, ok := op.Responses[strconv.Itoa(status)]; ok {
			continue
		}
		op.Responses[strconv.Itoa(status)] = &huma.Response{
			Description: description,
			Content:     map[string]*huma.MediaType{"application/json": {Schema: schema}},
		}
	}
}

func newError(status int, msg string, errs ...error) huma.StatusError {
	// Gin answered invalid requests with 400, Huma with 422. Other 422s are
	// returned by the handlers on purpose.
	if status == http.StatusUnprocessableEntity && msg == validationFailed {
		status = http.StatusBadRequest
	}

	// The errors of a 5xx are internal, e.g. from the database
	if status >= http.StatusInternalServerError {
		errs = nil
	}

	details := make([]string, 0, min(len(errs), maxErrorDetails+1))
	omitted := 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		if len(details) == maxErrorDetails {
			omitted++
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
	if omitted > 0 {
		details = append(details, fmt.Sprintf("and %d more", omitted))
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

func countOf(values []int, value int) int {
	n := 0
	for _, v := range values {
		if v == value {
			n++
		}
	}
	return n
}
