import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useEffect, useState } from "react";
import type { Log } from "@/lib/log";
import { Button } from "@/components/ui/button";
import { Download, Pause, Play, Trash2 } from "lucide-react";

interface Props {
    logs: Log[];
    setLogs: React.Dispatch<React.SetStateAction<Log[]>>;
    selectedContainer: string;
    setSelectedContainer: (container: string) => void;
    logsEndRef: React.RefObject<HTMLDivElement>;
}


export function Controls({ logs, setLogs, selectedContainer, setSelectedContainer, logsEndRef }: Props) {
    const [autoScroll, setAutoScroll] = useState(true)
    const [isPaused, setIsPaused] = useState(false)

    useEffect(() => {
        if (autoScroll && !isPaused) {
            logsEndRef.current?.scrollIntoView({ behavior: "smooth" })
        }
    }, [logs, autoScroll, isPaused])

    useEffect(() => {
        if (isPaused) return

        const interval = setInterval(() => {
            const newLog = {
                timestamp: new Date().toISOString(),
                container: ["api-gateway", "redis-cache", "nginx-proxy"][Math.floor(Math.random() * 3)],
                level: ["info", "debug", "warn"][Math.floor(Math.random() * 3)],
                message: `Request processed - ${Math.floor(Math.random() * 100)}ms`,
            }
            setLogs((prev) => [...prev.slice(-100), newLog])
        }, 2000)

        return () => clearInterval(interval)
    }, [isPaused])
    return (
        <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-center justify-between">
            <div className="flex flex-wrap gap-4 items-center">
                <Select value={selectedContainer} onValueChange={setSelectedContainer}>
                    <SelectTrigger className="w-40 bg-muted border-border">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">All Containers</SelectItem>
                        <SelectItem value="api-gateway">api-gateway</SelectItem>
                        <SelectItem value="redis-cache">redis-cache</SelectItem>
                        <SelectItem value="nginx-proxy">nginx-proxy</SelectItem>
                    </SelectContent>
                </Select>

                <div className="flex items-center gap-2">
                    <Switch id="autoscroll" checked={autoScroll} onCheckedChange={setAutoScroll} />
                    <Label htmlFor="autoscroll" className="text-sm">
                        Auto-scroll
                    </Label>
                </div>
            </div>
            <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => setIsPaused(!isPaused)}>
                    {isPaused ? <Play className="mr-2 h-4 w-4" /> : <Pause className="mr-2 h-4 w-4" />}
                    {isPaused ? "Resume" : "Pause"}
                </Button>
                <Button variant="outline" size="sm">
                    <Download className="mr-2 h-4 w-4" />
                    Download
                </Button>
                <Button variant="outline" size="sm" onClick={() => setLogs([])}>
                    <Trash2 className="mr-2 h-4 w-4" />
                    Clear
                </Button>
            </div>
        </div>
    )
}
