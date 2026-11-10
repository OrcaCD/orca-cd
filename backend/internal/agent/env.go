package agent

import (
	"context"
	"errors"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/envrules"
	"github.com/OrcaCD/orca-cd/internal/shared/utils"
)

const envListTimeout = 30 * time.Second

var envLockWaitTimeout = envrules.LockWaitTimeout

const errEnvUnsupported = "managing environment variables is not available on this agent"
const errEnvBusy = "the application is busy with another operation, please try again shortly"

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
	if !utils.LockWithTimeout(ctx, lock, envLockWaitTimeout) {
		result.ErrorMessage = errEnvBusy
		sendEnvUpdateResult(sender, result)
		return
	}
	defer lock.Unlock()

	writeCtx, cancelWrite := context.WithTimeout(ctx, envrules.WriteTimeout)
	defer cancelWrite()

	if err := manager.UpdateEnv(writeCtx, req.ApplicationName, req.Files); err != nil {
		result.Conflict = errors.Is(err, docker.ErrEnvConflict)
		result.ErrorMessage = err.Error()
		if !result.Conflict {
			Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("updating env files failed")
		}
		sendEnvUpdateResult(sender, result)
		return
	}

	redeployCtx, cancelRedeploy := context.WithTimeout(ctx, envrules.RedeployTimeout)
	defer cancelRedeploy()

	if err := manager.Redeploy(redeployCtx, req.ApplicationId, req.ApplicationName); err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("redeploy after env update failed")
		result.ErrorMessage = envrules.SavedRedeployFailedPrefix + ": " + err.Error()
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
