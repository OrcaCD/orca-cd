package agent

import (
	"context"
	"errors"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

const envListTimeout = 30 * time.Second
const envUpdateTimeout = 2 * time.Minute

const errEnvUnsupported = "managing environment variables is not available on this agent"

func executeEnvList(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.EnvListRequest) {
	Log.Debug().Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("listing env files")

	result := &messages.EnvListResult{
		RequestId:     req.RequestId,
		ApplicationId: req.ApplicationId,
	}

	manager, ok := deployer.(envManager)
	if !ok {
		result.ErrorMessage = errEnvUnsupported
		sendEnvListResult(sender, result)
		return
	}

	listCtx, cancel := context.WithTimeout(ctx, envListTimeout)
	defer cancel()

	files, err := manager.ListEnv(listCtx, req.ApplicationName)
	if err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("listing env files failed")
		result.ErrorMessage = err.Error()
		sendEnvListResult(sender, result)
		return
	}

	result.Success = true
	result.EnvFiles = files
	sendEnvListResult(sender, result)
}

func executeEnvUpdate(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.EnvUpdateRequest) {
	Log.Info().Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("updating env files")

	result := &messages.EnvUpdateResult{
		RequestId:     req.RequestId,
		ApplicationId: req.ApplicationId,
	}

	manager, ok := deployer.(envManager)
	if !ok {
		result.ErrorMessage = errEnvUnsupported
		sendEnvUpdateResult(sender, result)
		return
	}

	lock := lockForApplication(req.ApplicationId)
	lock.Lock()
	defer lock.Unlock()

	updateCtx, cancel := context.WithTimeout(ctx, envUpdateTimeout)
	defer cancel()

	if err := manager.UpdateEnv(updateCtx, req.ApplicationName, req.Files); err != nil {
		result.Conflict = errors.Is(err, docker.ErrEnvConflict)
		result.ErrorMessage = err.Error()
		if !result.Conflict {
			Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("updating env files failed")
		}
		sendEnvUpdateResult(sender, result)
		return
	}

	if err := manager.Redeploy(updateCtx, req.ApplicationId, req.ApplicationName); err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("redeploy after env update failed")
		result.ErrorMessage = "the changes were saved, but redeploying the application failed: " + err.Error()
		sendEnvUpdateResult(sender, result)
		return
	}

	result.Success = true
	sendEnvUpdateResult(sender, result)
}

func sendEnvListResult(sender outboundSender, result *messages.EnvListResult) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_EnvListResult{
			EnvListResult: result,
		},
	}); err != nil {
		Log.Error().
			Err(err).
			Str("application_id", result.ApplicationId).
			Str("request_id", result.RequestId).
			Msg("failed to send env list result")
	}
}

func sendEnvUpdateResult(sender outboundSender, result *messages.EnvUpdateResult) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_EnvUpdateResult{
			EnvUpdateResult: result,
		},
	}); err != nil {
		Log.Error().
			Err(err).
			Str("application_id", result.ApplicationId).
			Str("request_id", result.RequestId).
			Msg("failed to send env update result")
	}
}
