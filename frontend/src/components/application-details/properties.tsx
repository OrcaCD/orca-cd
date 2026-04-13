import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { Application } from "@/lib/applications";
import { Box, MoreVertical, RotateCcw, Square, Terminal } from "lucide-react";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Button } from "../ui/button";
import { Link } from "@tanstack/react-router";

interface Props {
    app: Application;
    id: string;
}

export function Properties({ app, id }: Props) {
    return (
        <Tabs defaultValue="containers" className="space-y-4">
            <TabsList className="bg-muted">
                <TabsTrigger value="containers">Containers</TabsTrigger>
                <TabsTrigger value="events">Events</TabsTrigger>
                <TabsTrigger value="logs">Logs</TabsTrigger>
                <TabsTrigger value="manifest">Manifest</TabsTrigger>
            </TabsList>

            <TabsContent value="containers" className="space-y-4">
                <div className="bg-card border border-border rounded-lg overflow-hidden">
                    <table className="w-full">
                        <thead>
                            <tr className="border-b border-border">
                                <th className="text-left p-4 text-sm font-medium text-muted-foreground">Container</th>
                                <th className="text-left p-4 text-sm font-medium text-muted-foreground hidden sm:table-cell">
                                    Image
                                </th>
                                <th className="text-left p-4 text-sm font-medium text-muted-foreground">Status</th>
                                <th className="text-left p-4 text-sm font-medium text-muted-foreground hidden md:table-cell">
                                    CPU
                                </th>
                                <th className="text-left p-4 text-sm font-medium text-muted-foreground hidden md:table-cell">
                                    Memory
                                </th>
                                <th className="text-left p-4 text-sm font-medium text-muted-foreground hidden lg:table-cell">
                                    Ports
                                </th>
                                <th className="p-4"></th>
                            </tr>
                        </thead>
                        <tbody>
                            {app.containers.map((container) => (
                                <tr key={container.id} className="border-b border-border last:border-0 hover:bg-muted/50">
                                    <td className="p-4">
                                        <div className="flex items-center gap-2">
                                            <Box className="h-4 w-4 text-primary" />
                                            <span className="font-medium">{container.name}</span>
                                        </div>
                                    </td>
                                    <td className="p-4 text-muted-foreground font-mono text-sm hidden sm:table-cell">
                                        {container.image}
                                    </td>
                                    <td className="p-4">
                                        <span className="inline-flex items-center gap-1.5 px-2 py-1 rounded-full text-xs font-medium bg-emerald-500/20 text-emerald-400 border border-emerald-500/30">
                                            <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                                            {container.status}
                                        </span>
                                    </td>
                                    <td className="p-4 text-muted-foreground text-sm hidden lg:table-cell">{container.ports}</td>
                                    <td className="p-4">
                                        <DropdownMenu>
                                            <DropdownMenuTrigger asChild>
                                                <Button variant="ghost" size="icon" className="h-8 w-8">
                                                    <MoreVertical className="h-4 w-4" />
                                                </Button>
                                            </DropdownMenuTrigger>
                                            <DropdownMenuContent align="end">
                                                <DropdownMenuItem>
                                                    <Terminal className="mr-2 h-4 w-4" />
                                                    View Logs
                                                </DropdownMenuItem>
                                                <DropdownMenuItem>
                                                    <RotateCcw className="mr-2 h-4 w-4" />
                                                    Restart
                                                </DropdownMenuItem>
                                                <DropdownMenuItem>
                                                    <Square className="mr-2 h-4 w-4" />
                                                    Stop
                                                </DropdownMenuItem>
                                            </DropdownMenuContent>
                                        </DropdownMenu>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </TabsContent>

            <TabsContent value="events" className="space-y-4">
                <div className="bg-card border border-border rounded-lg p-4 space-y-4">
                    {app.events.map((event, i) => (
                        <div key={i} className="flex items-start gap-3">
                            <div
                                className={`h-2 w-2 rounded-full mt-2 ${event.type === "success"
                                    ? "bg-emerald-400"
                                    : event.type === "warning"
                                        ? "bg-amber-400"
                                        : "bg-blue-400"
                                    }`}
                            />
                            <div className="flex-1">
                                <p className="text-sm">{event.message}</p>
                                <p className="text-xs text-muted-foreground mt-1">{event.time}</p>
                            </div>
                        </div>
                    ))}
                </div>
            </TabsContent>

            <TabsContent value="logs" className="space-y-4">
                <div className="bg-card border border-border rounded-lg p-4">
                    <Link to="/applications/$id/logs" params={{ id: id }}>
                        <Button className="w-full sm:w-auto">
                            <Terminal className="mr-2 h-4 w-4" />
                            Open Log Viewer
                        </Button>
                    </Link>
                </div>
            </TabsContent>

            <TabsContent value="manifest" className="space-y-4">
                <div className="bg-card border border-border rounded-lg p-4">
                    <pre className="text-sm font-mono text-muted-foreground overflow-x-auto">
                        {`version: "3.8"
services:
  api-gateway:
    image: org/api-gateway:v2.1.0
    ports:
      - "8080:80"
    environment:
      - NODE_ENV=production
    depends_on:
      - redis-cache

  redis-cache:
    image: redis:7-alpine
    volumes:
      - redis-data:/data

  nginx-proxy:
    image: nginx:alpine
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf

volumes:
  redis-data:`}
                    </pre>
                </div>
            </TabsContent>
        </Tabs>
    )
}
