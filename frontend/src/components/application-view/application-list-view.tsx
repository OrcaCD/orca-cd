import type { Application } from "@/lib/applications";
import { ApplicationTableRow } from "./application-table-row";

export function ApplicationListView({ apps }: { apps: Application[] }) {
    return (
        <div className="bg-card border border-border rounded-lg overflow-hidden">
            <table className="w-full">
                <tbody>
                    {apps.map((app) => (
                        <ApplicationTableRow key={app.id} app={app} />
                    ))}
                </tbody>
            </table>
        </div>
    )
}
