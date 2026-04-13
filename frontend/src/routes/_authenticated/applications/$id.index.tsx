import { Breadcrumb } from "@/components/application/details/breadcrumb"
import { Header } from "@/components/application/details/header"
import { InfoCards } from "@/components/application/details/info-cards"
import { Properties } from "@/components/application/details/properties"
import { HealthStatus, SyncStatus } from "@/lib/applications"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute('/_authenticated/applications/$id/')({
  component: ApplicationDetailsPage,
  head: () => ({
    meta: [
      {
        title: "Applications",
      },
    ],
  }),
})

const mockApp = {
  id: "1",
  name: "api-gateway",
  project: "production",
  syncStatus: SyncStatus.Synced,
  healthStatus: HealthStatus.Healthy,
  repo: "github.com/org/api-gateway",
  branch: "main",
  commit: "a3f2b1c",
  commitMessage: "fix: update rate limiting configuration",
  lastSync: "2 minutes ago",
  path: "/docker-compose.yml",
  targetHost: "prod-server-01",
  containers: [
    {
      id: "1",
      name: "api-gateway",
      image: "org/api-gateway:v2.1.0",
      status: "running",
      ports: "8080:80",
    },
    {
      id: "2",
      name: "redis-cache",
      image: "redis:7-alpine",
      status: "running",
      ports: "6379",
    },
    {
      id: "3",
      name: "nginx-proxy",
      image: "nginx:alpine",
      status: "running",
      ports: "443:443, 80:80",
    },
  ],
  events: [
    { time: "2m ago", message: "Sync completed successfully", type: "success" },
    { time: "2m ago", message: "Pulling image org/api-gateway:v2.1.0", type: "info" },
    { time: "3m ago", message: "Sync started", type: "info" },
    { time: "1h ago", message: "Health check passed", type: "success" },
    { time: "2h ago", message: "Container api-gateway restarted", type: "warning" },
  ],
}

export default function ApplicationDetailsPage() {
  const { id } = Route.useParams()
  return (
    <div className="p-6 space-y-6">
      <Breadcrumb app={mockApp} />
      <Header app={mockApp} id={id} />
      <InfoCards app={mockApp} />
      <Properties app={mockApp} id={id} />
    </div>
  )
}
