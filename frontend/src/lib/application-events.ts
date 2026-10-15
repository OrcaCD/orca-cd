import { m } from "@/lib/paraglide/messages";

export enum ApplicationEventType {
	Deployment = "deployment",
	CommitSync = "commit_sync",
	ImageUpdate = "image_update",
}

export enum ApplicationEventSource {
	Manual = "manual",
	ApplicationCreated = "application_created",
	RepositoryPolling = "repository_polling",
	RepositoryWebhook = "repository_webhook",
	GitHubActions = "github_actions",
	ImagePolling = "image_polling",
	ImageWebhook = "image_webhook",
}

export enum ApplicationEventStatus {
	Running = "running",
	Succeeded = "succeeded",
	Failed = "failed",
	NoChange = "no_change",
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
}

export interface ApplicationEventsPage {
	items: ApplicationEvent[];
	hasMore: boolean;
}

const typeLabels: Record<ApplicationEventType, () => string> = {
	[ApplicationEventType.Deployment]: () => m.eventTypeDeployment(),
	[ApplicationEventType.CommitSync]: () => m.eventTypeCommitSync(),
	[ApplicationEventType.ImageUpdate]: () => m.eventTypeImageUpdate(),
};

const sourceLabels: Record<ApplicationEventSource, () => string> = {
	[ApplicationEventSource.Manual]: () => m.eventSourceManual(),
	[ApplicationEventSource.ApplicationCreated]: () => m.eventSourceApplicationCreated(),
	[ApplicationEventSource.RepositoryPolling]: () => m.eventSourceRepositoryPolling(),
	[ApplicationEventSource.RepositoryWebhook]: () => m.eventSourceRepositoryWebhook(),
	[ApplicationEventSource.GitHubActions]: () => m.eventSourceGitHubActions(),
	[ApplicationEventSource.ImagePolling]: () => m.eventSourceImagePolling(),
	[ApplicationEventSource.ImageWebhook]: () => m.eventSourceImageWebhook(),
};

export function eventTypeLabel(type: ApplicationEventType): string {
	return typeLabels[type]?.() ?? type;
}

export function eventSourceLabel(source: ApplicationEventSource): string {
	return sourceLabels[source]?.() ?? source;
}
