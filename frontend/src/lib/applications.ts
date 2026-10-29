import { fetcher } from "./api";
import type { RepositorySyncType } from "./repositories";
import type { ApplicationEventSource, ApplicationEvent } from "./application-events";

export interface Application {
	id: string;
	icon: string;
	name: string;
	syncStatus: SyncStatus;
	healthStatus: HealthStatus;
	repositoryId: string;
	repositoryName: string;
	repositoryUrl: string;
	repositorySyncType: RepositorySyncType;
	branch: string;
	commit: string;
	commitMessage: string;
	lastSyncedAt?: string;
	lastSyncError?: string;
	lastSyncSource?: ApplicationEventSource;
	path: string;
	agentId: string;
	agentName: string;
	createdAt: string;
	updatedAt: string;
	composeFile: string;
	previousComposeFile?: string;
	imagePollEnabled: boolean;
	imagePollIntervalSeconds: number;
	deleteOldImages: boolean;
	imageWebhookEnabled: boolean;
	imageWebhookUrl?: string;
	currentDeploymentId: string | null;
	rolledBackAt: string | null;
	rolledBackCommit: string | null;
	autoSyncPaused: boolean;
	rollback: RollbackSettings;
}

export interface RollbackSettings {
	enabled: boolean;
	onDeployFailure: boolean;
	onUnhealthy: boolean;
	healthTimeoutSeconds: number;
	pauseAutoSync: boolean;
}

export interface ApplicationListItem {
	id: string;
	name: string;
	icon: string;
	syncStatus: SyncStatus;
	healthStatus: HealthStatus;
	repositoryId: string;
	repositoryName: string;
	agentId: string;
	agentName: string;
	branch: string;
	commit: string;
	lastSyncedAt?: string;
	lastSyncError?: string;
	path: string;
	rolledBackAt: string | null;
	rolledBackCommit: string | null;
}

export interface ApplicationActivityDay {
	date: string;
	succeeded: number;
	failed: number;
}

export interface ApplicationActivityDeployment extends ApplicationEvent {
	applicationId: string;
	applicationName: string;
	applicationIcon: string;
}

export interface ApplicationActivity {
	days: ApplicationActivityDay[];
	recentDeployments: ApplicationActivityDeployment[];
}

export enum SyncStatus {
	Synced = "synced",
	OutOfSync = "out_of_sync",
	Syncing = "syncing",
	Unknown = "unknown",
}

export enum HealthStatus {
	Healthy = "healthy",
	Unhealthy = "unhealthy",
	Stopped = "stopped",
	Degraded = "degraded",
	Unknown = "unknown",
}

export enum Type {
	Success = "success",
	Info = "info",
	Warning = "warning",
	Error = "error",
}

interface CreateApplicationRequest {
	name: string;
	icon: string;
	repositoryId: string;
	agentId: string;
	branch: string;
	path: string;
	imagePollEnabled: boolean;
	imagePollIntervalSeconds: number;
	deleteOldImages: boolean;
}

interface UpdateApplicationRequest {
	name: string;
	icon: string;
	repositoryId: string;
	agentId: string;
	branch: string;
	path: string;
	imagePollEnabled: boolean;
	imagePollIntervalSeconds: number;
	deleteOldImages: boolean;
}

interface DeployApplicationResponse {
	message: string;
}

export function createApplication(data: CreateApplicationRequest): Promise<Application> {
	return fetcher<Application>("/applications", "POST", data);
}

export function updateApplication(
	id: string,
	data: UpdateApplicationRequest,
): Promise<Application> {
	return fetcher<Application>(`/applications/${id}`, "PUT", data);
}

export function deleteApplication(id: string): Promise<void> {
	return fetcher(`/applications/${id}`, "DELETE");
}

export function deployApplication(id: string): Promise<DeployApplicationResponse> {
	return fetcher<DeployApplicationResponse>(`/applications/${id}/deploy`, "POST");
}

export function pullApplicationImages(id: string): Promise<DeployApplicationResponse> {
	return fetcher<DeployApplicationResponse>(`/applications/${id}/pull-images`, "POST");
}

export interface GenerateImageWebhookResponse {
	secret: string;
	webhookUrl: string;
}

export function generateImageWebhook(id: string): Promise<GenerateImageWebhookResponse> {
	return fetcher<GenerateImageWebhookResponse>(`/applications/${id}/image-webhook`, "POST");
}

export function revokeImageWebhook(id: string): Promise<void> {
	return fetcher(`/applications/${id}/image-webhook`, "DELETE");
}

export function rollbackApplication(
	id: string,
	deploymentId: string,
): Promise<DeployApplicationResponse> {
	return fetcher<DeployApplicationResponse>(
		`/applications/${id}/deployments/${deploymentId}/rollback`,
		"POST",
	);
}

export function resumeApplicationAutoSync(id: string): Promise<DeployApplicationResponse> {
	return fetcher<DeployApplicationResponse>(`/applications/${id}/resume-auto-sync`, "POST");
}

export function updateRollbackSettings(id: string, data: RollbackSettings): Promise<Application> {
	return fetcher<Application>(`/applications/${id}/rollback`, "PUT", data);
}
