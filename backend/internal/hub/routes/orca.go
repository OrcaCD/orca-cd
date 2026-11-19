package routes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type OrcaOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

func RegisterOrca(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "get-orca",
		Method:        http.MethodGet,
		Path:          "/orca",
		DefaultStatus: http.StatusTeapot,
		Summary:       "Meet the orca",
		Description:   "An easter egg. Always answers with 418 I'm a teapot, even though it's an orca.",
		Tags:          []string{"System"},
	}, Orca)
}

func Orca(_ context.Context, _ *struct{}) (*OrcaOutput, error) {
	out := &OrcaOutput{}
	out.Body.Message = "I'm not a teapot, I'm an orca 🐋"
	return out, nil
}
