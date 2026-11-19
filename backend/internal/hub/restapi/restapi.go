// Package restapi sets up Huma, which describes the /api/v1 routes as an
// OpenAPI spec. The routes move to it one by one, so its defaults are changed
// to behave like the Gin handlers: the frontend and API clients must not notice
// which one answers a request.
package restapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/v1"

// New mounts a Huma API on the /api/v1 group of the router.
func New(router *gin.Engine, group *gin.RouterGroup) huma.API {
	config := huma.DefaultConfig("OrcaCD API", version.Version)
	config.Info.Description = "The REST API of the OrcaCD hub."
	config.Servers = []*huma.Server{{URL: basePath}}

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
	config.AllowAdditionalPropertiesByDefault = true
	config.FieldsOptionalByDefault = true

	return humagin.NewWithGroup(router, group, config)
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
