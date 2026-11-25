package agent

import (
	"context"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

const containerRequestTimeout = 30 * time.Second

const errContainersUnsupported = "inspecting containers is not available on this agent"

func executeContainerList(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.ContainerListRequest) {
	Log.Debug().Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("listing containers")

	result := &messages.ContainerListResult{
		RequestId:     req.RequestId,
		ApplicationId: req.ApplicationId,
	}

	manager, ok := deployer.(containerManager)
	if !ok {
		result.ErrorMessage = errContainersUnsupported
		sendContainerListResult(sender, result)
		return
	}

	listCtx, cancel := context.WithTimeout(ctx, containerRequestTimeout)
	defer cancel()

	containers, err := manager.ListContainers(listCtx, req.ApplicationId)
	if err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("listing containers failed")
		result.ErrorMessage = err.Error()
		sendContainerListResult(sender, result)
		return
	}

	result.Success = true
	result.Containers = containers
	sendContainerListResult(sender, result)
}

func executeContainerInspect(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.ContainerInspectRequest) {
	Log.Debug().Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("inspecting container")

	result := &messages.ContainerInspectResult{
		RequestId:     req.RequestId,
		ApplicationId: req.ApplicationId,
	}

	manager, ok := deployer.(containerManager)
	if !ok {
		result.ErrorMessage = errContainersUnsupported
		sendContainerInspectResult(sender, result)
		return
	}

	inspectCtx, cancel := context.WithTimeout(ctx, containerRequestTimeout)
	defer cancel()

	inspectJSON, err := manager.InspectContainer(inspectCtx, req.ApplicationId, req.ContainerId)
	if err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("inspecting container failed")
		result.ErrorMessage = err.Error()
		sendContainerInspectResult(sender, result)
		return
	}

	result.Success = true
	result.InspectJson = inspectJSON
	sendContainerInspectResult(sender, result)
}

func sendContainerListResult(sender outboundSender, result *messages.ContainerListResult) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ContainerListResult{
			ContainerListResult: result,
		},
	}); err != nil {
		Log.Error().
			Err(err).
			Str("application_id", result.ApplicationId).
			Str("request_id", result.RequestId).
			Msg("failed to send container list result")
	}
}

func sendContainerInspectResult(sender outboundSender, result *messages.ContainerInspectResult) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ContainerInspectResult{
			ContainerInspectResult: result,
		},
	}); err != nil {
		Log.Error().
			Err(err).
			Str("application_id", result.ApplicationId).
			Str("request_id", result.RequestId).
			Msg("failed to send container inspect result")
	}
}
