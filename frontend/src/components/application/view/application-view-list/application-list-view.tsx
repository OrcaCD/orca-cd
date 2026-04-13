import type { Application } from "@/lib/applications"
import { ApplicationTable } from "@/components/application/view/application-view-list/application-table"

export function ApplicationListView({ apps }: { apps: Application[] }) {
    return <ApplicationTable apps={apps} />
}
