import { HealthStatus, SyncStatus, type Application } from "@/lib/applications";

export function ApplicationStats({ apps }: { apps: Application[] }) {
    const stats = {
        total: apps.length,
        synced: apps.filter((a) => a.syncStatus === SyncStatus.Synced).length,
        outOfSync: apps.filter((a) => a.syncStatus === SyncStatus.OutOfSync).length,
        healthy: apps.filter((a) => a.healthStatus === HealthStatus.Healthy).length,
    }
    const statItems = [
        { label: "Total Apps", value: stats.total },
        { label: "Synced", value: stats.synced, color: "text-emerald-400" },
        { label: "Out of Sync", value: stats.outOfSync, color: "text-amber-400" },
        { label: "Healthy", value: stats.healthy, color: "text-emerald-400" },
    ]
    return (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            {statItems.map((item) => (
                <div
                    key={item.label}
                    className="bg-card border border-border rounded-lg p-4"
                >
                    <p className="text-muted-foreground text-sm">{item.label}</p>
                    <p className={`text-2xl font-bold mt-1 ${item.color ?? ""}`}>
                        {item.value}
                    </p>
                </div>
            ))}
        </div>
    );
}
