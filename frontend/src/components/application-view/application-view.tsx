import type { Application } from "@/lib/applications"
import { ApplicationGridView } from "./application-grid-view"
import { ApplicationListView } from "./application-list-view"

type Props = {
  viewMode: "grid" | "list"
  apps: Application[]
}

export function ApplicationGrid({ viewMode, apps }: Props) {
  if (viewMode === "grid") {
    return <ApplicationGridView apps={apps} />
  }

  return <ApplicationListView apps={apps} />
}
