package routes

import (
	"context"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
)

type healthOutput struct {
	Body struct {
		Status string `json:"status" required:"true" enum:"ok" doc:"Always ok while the hub is running"`
	}
}

var HealthHandler = restapi.Handler(huma.Operation{
	OperationID: "get-health",
	Method:      http.MethodGet,
	Path:        "/health",
	Summary:     "Check that the hub is running",
	Description: "Needs no authentication, e.g. for container health checks.",
	Tags:        []string{tagSystem},
	Security:    restapi.Public,
}, func(_ context.Context, _ *struct{}) (*healthOutput, error) {
	out := &healthOutput{}
	out.Body.Status = "ok"
	return out, nil
})
