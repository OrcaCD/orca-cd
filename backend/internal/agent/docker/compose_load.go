package docker

import (
	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/docker/compose/v5/pkg/api"
)

func composeProjectLoadOptions(projectName, composePath, workingDir string) api.ProjectLoadOptions {
	return api.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{composePath},
		WorkingDir:  workingDir,
		ProjectOptionsFns: []cli.ProjectOptionsFn{
			cli.WithLoadOptions(func(options *loader.Options) {
				options.SkipInclude = true
				options.SkipExtends = true
				options.SkipResolveLabels = true
			}),
		},
	}
}
