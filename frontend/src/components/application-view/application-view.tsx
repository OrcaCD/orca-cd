import type { Application } from "@/lib/applications"
import { ApplicationGridView } from "./application-grid-view"
import { ApplicationListView } from "./application-list-view"

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
