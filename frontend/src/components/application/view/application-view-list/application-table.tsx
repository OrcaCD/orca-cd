import type { Application } from "@/lib/applications"
import { ApplicationTableRow } from "@/components/application/view/application-view-list/application-table-row"

type Props = {
    apps: Application[]
}

export function ApplicationTable({ apps }: Props) {
    return (
        <div className="bg-card border border-border rounded-lg overflow-hidden">
            <table className="w-full">
                <thead>
                    <tr className="border-b border-border">
                        <th className="text-left p-4 text-sm font-medium text-muted-foreground">Name</th>
                        <th className="text-left p-4 text-sm font-medium text-muted-foreground hidden sm:table-cell">
                            Project
                        </th>
                        <th className="text-left p-4 text-sm font-medium text-muted-foreground">Status</th>
                        <th className="text-left p-4 text-sm font-medium text-muted-foreground hidden md:table-cell">
                            Repository
                        </th>
                        <th className="text-left p-4 text-sm font-medium text-muted-foreground hidden lg:table-cell">
                            Last Sync
                        </th>
                        <th className="p-4"></th>
                    </tr>
                </thead>
                <tbody>
                    {apps.map((app) => (
                        <ApplicationTableRow key={app.id} app={app} />
                    ))}
                </tbody>
            </table>
        </div>
    )
}
