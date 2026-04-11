import type { Application } from "@/lib/applications";
import { ApplicationCard } from "./application-card";

export function ApplicationGridView({ apps }: { apps: Application[] }) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {apps.map((app) => (
        <ApplicationCard key={app.id} app={app} />
      ))}
    </div>
  )
}
