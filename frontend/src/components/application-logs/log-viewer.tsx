import type { Log } from "@/lib/log";

interface Props {
    logs: Log[];
    selectedContainer: string;
    logsEndRef: React.RefObject<HTMLDivElement>;
}

export function LogViewer({ logs, selectedContainer, logsEndRef }: Props) {
    const levelColors: Record<string, string> = {
        info: "text-blue-400",
        debug: "text-zinc-400",
        warn: "text-amber-400",
        error: "text-red-400",
    }
    const filteredLogs = selectedContainer === "all" ? logs : logs.filter((log) => log.container === selectedContainer)
    return (
        <div className="flex-1 bg-card border border-border rounded-lg overflow-hidden">
            <div className="h-full overflow-auto p-4 font-mono text-sm">
                {filteredLogs.map((log, i) => (
                    <div key={i} className="flex gap-4 py-1 hover:bg-muted/50 px-2 -mx-2 rounded">
                        <span className="text-muted-foreground shrink-0">{new Date(log.timestamp).toLocaleTimeString()}</span>
                        <span className="text-primary shrink-0">[{log.container}]</span>
                        <span className={`shrink-0 uppercase text-xs font-bold ${levelColors[log.level]}`}>{log.level}</span>
                        <span className="text-foreground">{log.message}</span>
                    </div>
                ))}
                <div ref={logsEndRef} />
            </div>
        </div>
    )
}
