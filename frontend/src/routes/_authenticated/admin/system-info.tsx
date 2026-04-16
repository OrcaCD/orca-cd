import { createFileRoute } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { useFetch } from "@/lib/api";

interface SystemInfo {
	debug: boolean;
	host: string;
	port: string;
	logLevel: string;
	trustedProxies: string[];
	appUrl: string;
	disableLocalAuth: boolean;
	version: string;
	commit: string;
	buildDate: string;
}

interface GitHubRelease {
	tag_name: string;
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

function SystemVersion({ systemInfo }: { systemInfo: SystemInfo }) {
	const { data, isLoading } = useFetch<GitHubRelease>(
		"https://api.github.com/repos/OrcaCD/orca-cd/releases/latest",
	);

	if (isLoading || !data) {
		return null;
	}

	if (data.tag_name === systemInfo.version) {
		return (
			<div className="text-xs text-green-500 font-medium flex gap-1">
				<CircleCheck className="w-4 h-4" />
				<span>Up to date</span>
			</div>
		);
	}

	return (
		<a
			href="https://github.com/OrcaCD/orca-cd/releases"
			target="_blank"
			rel="noopener noreferrer"
			className="text-xs text-red-500 font-medium flex gap-1"
		>
			<TriangleAlert className="w-4 h-4" />
			<span>Update available: {data.tag_name}</span>
		</a>
	);
}

function SystemInfoPage() {
	const { data, isLoading } = useFetch<SystemInfo>("/admin/system-info");
	const isTagVersion = data?.version.startsWith("v");

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
						<CardDescription>
							<div className="flex flex-col gap-1">
								<span>
									Version: {data.version} (commit: {data.commit}, built:{" "}
									{new Date(data.buildDate).toLocaleString()})
								</span>
								{isTagVersion ? <SystemVersion systemInfo={data} /> : null}
							</div>
						</CardDescription>
					</CardHeader>
					<Separator />
					<CardContent className="pt-4">
						<div className="flex flex-col gap-4">
							<InfoRow label="App URL">
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs break-all">
									{data.appUrl || "—"}
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
								<Badge variant="secondary">{data.logLevel}</Badge>
							</InfoRow>

							<InfoRow label="Password Authentication">
								<Badge variant={data.disableLocalAuth ? "destructive" : "outline"}>
									{data.disableLocalAuth ? "Enabled" : "Disabled"}
								</Badge>
							</InfoRow>

							<InfoRow label="Trusted Proxies">
								{data.trustedProxies && data.trustedProxies.length > 0 ? (
									<div className="flex flex-wrap gap-1.5">
										{data.trustedProxies.map((proxy) => (
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
