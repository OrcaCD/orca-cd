import type { Application } from "@/lib/applications"
import { ApplicationTable } from "./application-table"

export function ApplicationListView({ apps }: { apps: Application[] }) {
    return <ApplicationTable apps={apps} />
}
