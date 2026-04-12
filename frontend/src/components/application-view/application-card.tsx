import type { Application } from "@/lib/applications"
import { Box, GitBranch, GitCommit } from "lucide-react"
import { Link } from "@tanstack/react-router"
import { ActionsMenu } from "./actions-menu"
import { StatusBadge } from "@/components/status-badge"

export function ApplicationCard({ app }: { app: Application }) {
    return (
        <Link
            key={app.id}
            to={`/applications/$id`}
            params={{ id: app.id }}
            className="group bg-card border border-border rounded-lg p-4 hover:border-primary/50 transition-colors"
        >
            <div className="flex items-start justify-between">
                <div className="flex items-center gap-3">
                    <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
                        <Box className="h-5 w-5 text-primary" />
                    </div>
                    <div>
                        <h3 className="font-medium group-hover:text-primary transition-colors">{app.name}</h3>
                        <p className="text-xs text-muted-foreground">{app.project}</p>
                    </div>
                </div>
                <ActionsMenu />
            </div>

            <div className="flex gap-2 mt-4">
                <StatusBadge status={app.syncStatus} />
                <StatusBadge status={app.healthStatus} />
            </div>

            <div className="mt-4 pt-4 border-t border-border space-y-2">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <GitBranch className="h-4 w-4" />
                    <span className="truncate">{app.repo}</span>
                </div>
                <div className="flex items-center justify-between text-sm">
                    <div className="flex items-center gap-2 text-muted-foreground">
                        <GitCommit className="h-4 w-4" />
                        <span>{app.commit}</span>
                    </div>
                    <span className="text-muted-foreground">{app.lastSync}</span>
                </div>
            </div>
        </Link>
    )
}
