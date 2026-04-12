export interface Log {
    timestamp: string
    container: string
    level: Level
    message: string
}

export enum Level {
    Info = "info",
    Warning = "warning",
    Error = "error",
}
