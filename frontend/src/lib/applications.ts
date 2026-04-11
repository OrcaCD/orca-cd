export interface Application {
    id: string
    name: string
    project: string
    syncStatus: SyncStatus
    healthStatus: HealthStatus
    repo: string
    branch: string
    commit: string
    lastSync: string
    containers: number
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