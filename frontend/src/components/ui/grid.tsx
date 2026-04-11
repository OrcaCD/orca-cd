import { HealthStatus, SyncStatus, type Application } from "@/lib/applications";

export function ApplicationStats({ apps }: { apps: Application[] }) {
    const stats = {
        total: apps.length,
        synced: apps.filter((a) => a.syncStatus === SyncStatus.Synced).length,
        outOfSync: apps.filter((a) => a.syncStatus === SyncStatus.OutOfSync).length,
        healthy: apps.filter((a) => a.healthStatus === HealthStatus.Healthy).length,
    }
    return (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            <div className="bg-card border border-border rounded-lg p-4">
                <p className="text-muted-foreground text-sm">Total Apps</p>
                <p className="text-2xl font-bold mt-1">{stats.total}</p>
            </div>
            <div className="bg-card border border-border rounded-lg p-4">
                <p className="text-muted-foreground text-sm">Synced</p>
                <p className="text-2xl font-bold mt-1 text-emerald-400">{stats.synced}</p>
            </div>
            <div className="bg-card border border-border rounded-lg p-4">
                <p className="text-muted-foreground text-sm">Out of Sync</p>
                <p className="text-2xl font-bold mt-1 text-amber-400">{stats.outOfSync}</p>
            </div>
            <div className="bg-card border border-border rounded-lg p-4">
                <p className="text-muted-foreground text-sm">Healthy</p>
                <p className="text-2xl font-bold mt-1 text-emerald-400">{stats.healthy}</p>
            </div>
        </div>
    );
}
