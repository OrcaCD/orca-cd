package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type HealthOutput struct {
	Body struct {
		Status string `json:"status" enum:"ok" doc:"Always ok while the hub is running"`
	}
}

func RegisterHealth(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Check that the hub is running",
		Description: "Needs no authentication, e.g. for container health checks.",
		Tags:        []string{"System"},
	}, Health)
}

func Health(_ context.Context, _ *struct{}) (*HealthOutput, error) {
	out := &HealthOutput{}
	out.Body.Status = "ok"
	return out, nil
}
