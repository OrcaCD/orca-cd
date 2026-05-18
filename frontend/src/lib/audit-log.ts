import type { UserDetail } from "./users";

export interface AuditLog {
    id: string;
    createdAt: string;
    updatedAt: string;
    userId: string;
    user: UserDetail;
    eventType: EventType;
    targetType: TargetType;
    targetId: string;
}

export enum TargetType {
    Agent = "agent",
    Application = "application",
    Repository = "repository",
    User = "user",
    OidcProvider = "oidc-provider",
}

export enum EventType {
    Created = "created",
    Updated = "updated",
    Deleted = "deleted",
}
