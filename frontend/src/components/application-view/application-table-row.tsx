import { Link } from "lucide-react";
import { ActionsMenu } from "@/components/application-view/index";
import  { StatusBadge } from "@/components/status-badge";
import type { Application } from "@/lib/applications";

export function ApplicationTableRow({ app }: { app: Application }) {
    return (
        <tr className="border-b border-border last:border-0 hover:bg-muted/50">
            <td className="p-4">
                <Link href={`/applications/${app.id}`} className="font-medium hover:text-primary">
                    {app.name}
                </Link>
            </td>

            <td className="p-4 text-muted-foreground hidden sm:table-cell">
                {app.project}
            </td>

            <td className="p-4">
                <div className="flex gap-2 flex-wrap">
                    <StatusBadge status={app.syncStatus} />
                    <StatusBadge status={app.healthStatus} />
                </div>
            </td>

            <td className="p-4 text-muted-foreground hidden md:table-cell">
                {app.repo}
            </td>

            <td className="p-4 text-muted-foreground hidden lg:table-cell">
                {app.lastSync}
            </td>

            <td className="p-4">
                <ActionsMenu app={app} />
            </td>
        </tr>
    )
}
