import { m } from "@/lib/paraglide/messages";

export enum ApplicationEventType {
	Deployment = "deployment",
	CommitSync = "commit_sync",
	ImageUpdate = "image_update",
	Rollback = "rollback",
}

export enum ApplicationEventSource {
	Manual = "manual",
	ApplicationCreated = "application_created",
	RepositoryPolling = "repository_polling",
	RepositoryWebhook = "repository_webhook",
	GitHubActions = "github_actions",
	ImagePolling = "image_polling",
	ImageWebhook = "image_webhook",
	DeploymentFailure = "deployment_failure",
	HealthCheck = "health_check",
}

export enum ApplicationEventStatus {
	Running = "running",
	Succeeded = "succeeded",
	Failed = "failed",
	NoChange = "no_change",
}

export enum DeploymentStatus {
	Pending = "pending",
	Verifying = "verifying",
	Succeeded = "succeeded",
}

export interface ApplicationEvent {
	id: string;
	createdAt: string;
	completedAt: string | null;
	type: ApplicationEventType;
	source: ApplicationEventSource;
	status: ApplicationEventStatus;
	actorName: string | null;
	commitHash: string | null;
	commitMessage: string | null;
	errorMessage: string | null;
	deploymentId: string | null;
	deploymentStatus: DeploymentStatus | null;
}

export interface ApplicationEventsPage {
	items: ApplicationEvent[];
	hasMore: boolean;
}

const typeLabels: Record<ApplicationEventType, () => string> = {
	[ApplicationEventType.Deployment]: () => m.eventTypeDeployment(),
	[ApplicationEventType.CommitSync]: () => m.eventTypeCommitSync(),
	[ApplicationEventType.ImageUpdate]: () => m.eventTypeImageUpdate(),
	[ApplicationEventType.Rollback]: () => m.eventTypeRollback(),
};

const sourceLabels: Record<ApplicationEventSource, () => string> = {
	[ApplicationEventSource.Manual]: () => m.eventSourceManual(),
	[ApplicationEventSource.ApplicationCreated]: () => m.eventSourceApplicationCreated(),
	[ApplicationEventSource.RepositoryPolling]: () => m.eventSourceRepositoryPolling(),
	[ApplicationEventSource.RepositoryWebhook]: () => m.eventSourceRepositoryWebhook(),
	[ApplicationEventSource.GitHubActions]: () => m.eventSourceGitHubActions(),
	[ApplicationEventSource.ImagePolling]: () => m.eventSourceImagePolling(),
	[ApplicationEventSource.ImageWebhook]: () => m.eventSourceImageWebhook(),
	[ApplicationEventSource.DeploymentFailure]: () => m.eventSourceDeploymentFailure(),
	[ApplicationEventSource.HealthCheck]: () => m.eventSourceHealthCheck(),
};

export function eventTypeLabel(type: ApplicationEventType): string {
	return typeLabels[type]?.() ?? type;
}

export function eventSourceLabel(source: ApplicationEventSource): string {
	return sourceLabels[source]?.() ?? source;
}

// isAutomaticRollback reports whether OrcaCD rolled back on its own after a
// failed deployment.
export function isAutomaticRollback(event: ApplicationEvent): boolean {
	return (
		event.type === ApplicationEventType.Rollback &&
		(event.source === ApplicationEventSource.DeploymentFailure ||
			event.source === ApplicationEventSource.HealthCheck)
	);
}

// eventTitle names the event, and for rollbacks the commit they restored.
export function eventTitle(event: ApplicationEvent): string {
	if (event.type !== ApplicationEventType.Rollback) {
		return eventTypeLabel(event.type);
	}
	const commit = event.commitHash?.slice(0, 7) ?? "";
	return isAutomaticRollback(event)
		? m.historyAutomaticRollbackTo({ commit })
		: m.historyRollbackTo({ commit });
}

// canRollBackTo reports whether the deployment of an event can be restored.
export function canRollBackTo(event: ApplicationEvent, currentDeploymentId?: string | null) {
	return (
		event.type !== ApplicationEventType.Rollback &&
		event.deploymentId !== null &&
		event.deploymentStatus === DeploymentStatus.Succeeded &&
		event.deploymentId !== currentDeploymentId
	);
}

// isCurrentDeployment reports whether the event deployed what runs now.
export function isCurrentDeployment(event: ApplicationEvent, currentDeploymentId?: string | null) {
	return (
		event.type !== ApplicationEventType.Rollback &&
		event.deploymentId !== null &&
		event.deploymentId === currentDeploymentId
	);
}
