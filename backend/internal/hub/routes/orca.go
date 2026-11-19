package routes

import (
	"context"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
)

type orcaOutput struct {
	Body struct {
		Message string `json:"message" required:"true"`
	}
}

var OrcaHandler = restapi.Handler(huma.Operation{
	OperationID:   "get-orca",
	Method:        http.MethodGet,
	Path:          "/orca",
	DefaultStatus: http.StatusTeapot,
	Summary:       "Meet the orca",
	Description:   "An easter egg. Always answers with 418 I'm a teapot, even though it's an orca.",
	Tags:          []string{tagSystem},
	Security:      restapi.Public,
}, func(_ context.Context, _ *struct{}) (*orcaOutput, error) {
	out := &orcaOutput{}
	out.Body.Message = "I'm not a teapot, I'm an orca 🐋"
	return out, nil
})
