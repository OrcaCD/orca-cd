import { createFileRoute } from "@tanstack/react-router";
import useSWR from "swr";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import fetcher, { API_BASE } from "@/lib/api";

interface SystemInfo {
	debug: boolean;
	host: string;
	port: string;
	log_level: string;
	trusted_proxies: string[];
	app_url: string;
	disable_local_auth: boolean;
	version: string;
}

export const Route = createFileRoute("/_authenticated/admin/system-info")({
	component: SystemInfoPage,
	head: () => ({
		meta: [
			{
				title: "Admin - System Info",
			},
		],
	}),
});

function InfoRow({ label, children }: { label: string; children: React.ReactNode }) {
	return (
		<div className="grid gap-1 sm:grid-cols-[12rem_minmax(0,1fr)] sm:items-start sm:gap-3">
			<span className="text-muted-foreground text-sm sm:pt-0.5">{label}</span>
			<div className="min-w-0 text-sm">{children}</div>
		</div>
	);
}

function SystemInfoPage() {
	const { data, isLoading } = useSWR<SystemInfo>(`${API_BASE}/admin/system-info`, fetcher);

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">System Information</h1>
				<p className="text-muted-foreground text-sm">
					Current server configuration and environment settings.
				</p>
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">Loading system info...</p>}

			{data && (
				<Card>
					<CardHeader>
						<CardTitle>Server Configuration</CardTitle>
						<CardDescription>Version: {data.version}</CardDescription>
					</CardHeader>
					<Separator />
					<CardContent className="pt-4">
						<div className="flex flex-col gap-4">
							<InfoRow label="App URL">
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs break-all">
									{data.app_url || "—"}
								</code>
							</InfoRow>

							<InfoRow label="Host">
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
									{data.host || "0.0.0.0"}
								</code>
							</InfoRow>

							<InfoRow label="Port">
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
									{data.port}
								</code>
							</InfoRow>

							<InfoRow label="Log Level">
								<Badge variant="secondary">{data.log_level}</Badge>
							</InfoRow>

							<InfoRow label="Debug Mode">
								<Badge variant={data.debug ? "success" : "outline"}>
									{data.debug ? "Enabled" : "Disabled"}
								</Badge>
							</InfoRow>

							<InfoRow label="Password Authentication">
								<Badge variant={data.disable_local_auth ? "destructive" : "outline"}>
									{data.disable_local_auth ? "Enabled" : "Disabled"}
								</Badge>
							</InfoRow>

							<InfoRow label="Trusted Proxies">
								{data.trusted_proxies && data.trusted_proxies.length > 0 ? (
									<div className="flex flex-wrap gap-1.5">
										{data.trusted_proxies.map((proxy) => (
											<code
												key={proxy}
												className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs"
											>
												{proxy}
											</code>
										))}
									</div>
								) : (
									<span className="text-muted-foreground text-xs italic">None configured</span>
								)}
							</InfoRow>
						</div>
					</CardContent>
				</Card>
			)}
		</div>
	);
}
