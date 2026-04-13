export interface Application {
  id: string
  name: string
  project: string
  syncStatus: SyncStatus
  healthStatus: HealthStatus
  repo: string
  branch: string
  commit: string
  commitMessage: string
  lastSync: string
  path: string
  targetHost: string
  containers: Container[]
  events: Event[]
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

export interface Container {
  id: string
  name: string
  image: string
  status: string
  ports: string
}

export interface Event {
  time: string
  message: string
  type: Type
}

export enum Type {
  Success = "success",
  Info = "info",
  Warning = "warning",
  Error = "error",
}
