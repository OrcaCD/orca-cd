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
