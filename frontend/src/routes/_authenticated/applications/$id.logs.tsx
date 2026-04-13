import { Breadcrumb } from '@/components/application/logs/breadcrumb'
import { Controls } from '@/components/application/logs/controls'
import { LogViewer } from '@/components/application/logs/log-viewer'
import { createFileRoute } from '@tanstack/react-router'
import { useRef, useState } from 'react'

export const Route = createFileRoute('/_authenticated/applications/$id/logs')({
  component: LogsPage,
  head: () => ({
    meta: [
      {
        title: "Logs",
      },
    ],
  }),
})

const mockLogs = [
  {
    timestamp: "2024-01-15T10:23:45Z",
    container: "api-gateway",
    level: "info",
    message: "Server started on port 8080",
  },
  { timestamp: "2024-01-15T10:23:46Z", container: "api-gateway", level: "info", message: "Connected to Redis cache" },
  { timestamp: "2024-01-15T10:23:47Z", container: "nginx-proxy", level: "info", message: "Nginx configuration loaded" },
  {
    timestamp: "2024-01-15T10:23:48Z",
    container: "api-gateway",
    level: "debug",
    message: "Health check endpoint registered",
  },
  {
    timestamp: "2024-01-15T10:24:00Z",
    container: "api-gateway",
    level: "info",
    message: "GET /api/v1/users - 200 OK - 45ms",
  },
  {
    timestamp: "2024-01-15T10:24:01Z",
    container: "redis-cache",
    level: "info",
    message: "Cache hit for key: user:123",
  },
  {
    timestamp: "2024-01-15T10:24:05Z",
    container: "api-gateway",
    level: "warn",
    message: "Rate limit approaching for IP 192.168.1.50",
  },
  {
    timestamp: "2024-01-15T10:24:10Z",
    container: "api-gateway",
    level: "info",
    message: "POST /api/v1/orders - 201 Created - 120ms",
  },
  {
    timestamp: "2024-01-15T10:24:15Z",
    container: "nginx-proxy",
    level: "info",
    message: "SSL certificate valid, expires in 45 days",
  },
  {
    timestamp: "2024-01-15T10:24:20Z",
    container: "api-gateway",
    level: "error",
    message: "Database connection timeout - retrying in 5s",
  },
  {
    timestamp: "2024-01-15T10:24:25Z",
    container: "api-gateway",
    level: "info",
    message: "Database connection restored",
  },
  {
    timestamp: "2024-01-15T10:24:30Z",
    container: "api-gateway",
    level: "info",
    message: "GET /api/v1/products - 200 OK - 32ms",
  },
]

function LogsPage() {
  const { id } = Route.useParams()
  const [selectedContainer, setSelectedContainer] = useState("all")
  const [logs, setLogs] = useState(mockLogs)
  const logsEndRef = useRef<HTMLDivElement>(null)
  return (
    <div className="p-6 space-y-4 h-[calc(100vh-3.5rem)] flex flex-col">
      <Breadcrumb id={id} />
      <Controls logs={logs} setLogs={setLogs} selectedContainer={selectedContainer} setSelectedContainer={setSelectedContainer} logsEndRef={logsEndRef} />
      <LogViewer logs={logs} selectedContainer={selectedContainer} logsEndRef={logsEndRef} />
    </div>
  )
}
