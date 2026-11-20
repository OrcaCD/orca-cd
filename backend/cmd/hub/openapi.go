package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/spf13/cobra"
)

func newOpenAPICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec of the REST API",
		Long:  "Print the OpenAPI spec of the /api/v1 routes as JSON. It is generated from the route definitions, without a configuration or database.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeOpenAPI(cmd.OutOrStdout())
		},
	}
}

func writeOpenAPI(out io.Writer) error {
	spec, err := json.MarshalIndent(restapi.OpenAPI(), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the OpenAPI spec: %w", err)
	}
	_, err = fmt.Fprintf(out, "%s\n", spec)
	return err
}
