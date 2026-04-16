export interface Application {
	id: string;
	name: string;
	syncStatus: SyncStatus;
	healthStatus: HealthStatus;
	repo: string;
	branch: string;
	commit: string;
	commitMessage: string;
	lastSync: string;
	path: string;
	agent: string;
}

export enum SyncStatus {
	Synced = "Synced",
	OutOfSync = "OutOfSync",
	Progressing = "Progressing",
	Unknown = "Unknown",
}

export enum HealthStatus {
	Healthy = "Healthy",
	Degraded = "Degraded",
	Progressing = "Progressing",
	Unknown = "Unknown",
}

export enum Type {
	Success = "success",
	Info = "info",
	Warning = "warning",
	Error = "error",
}
