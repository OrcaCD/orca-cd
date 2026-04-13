import type { Application } from "@/lib/applications"
import { ApplicationGridView } from "@/components/application/view/application-view-grid/application-grid-view"
import { ApplicationListView } from "@/components/application/view/application-view-list/application-list-view"

type Props = {
  viewMode: "grid" | "list"
  apps: Application[]
}

export function ApplicationView({ viewMode, apps }: Props) {
  return viewMode === "grid" ? (
    <ApplicationGridView apps={apps} />
  ) : (
    <ApplicationListView apps={apps} />
  )
}
