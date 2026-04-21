import { createFileRoute } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";

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
				title: `${m.admin()} - ${m.adminSystemInfo()}`,
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
				<span>{m.upToDate()}</span>
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
			<span>{m.updateAvailable({ version: data.tag_name })}</span>
		</a>
	);
}

function SystemInfoPage() {
	const { data, isLoading } = useFetch<SystemInfo>("/admin/system-info");
	const isTagVersion = data?.version.startsWith("v");

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">{m.adminSystemInfo()}</h1>
				<p className="text-muted-foreground text-sm">{m.adminSystemInfoDescription()}</p>
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">{m.loadingSystemInfo()}</p>}

			{data && (
				<Card>
					<CardHeader>
						<CardTitle>{m.serverConfiguration()}</CardTitle>
						<CardDescription>
							<div className="flex flex-col gap-1">
								<span>
									{m.versionDetails({
										version: data.version,
										commit: data.commit,
										buildDate: new Date(data.buildDate).toLocaleString(),
									})}
								</span>
								{isTagVersion ? <SystemVersion systemInfo={data} /> : null}
							</div>
						</CardDescription>
					</CardHeader>
					<Separator />
					<CardContent className="pt-4">
						<div className="flex flex-col gap-4">
							<InfoRow label={m.appUrl()}>
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs break-all">
									{data.appUrl || "—"}
								</code>
							</InfoRow>

							<InfoRow label={m.host()}>
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
									{data.host || "0.0.0.0"}
								</code>
							</InfoRow>

							<InfoRow label={m.port()}>
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
									{data.port}
								</code>
							</InfoRow>

							<InfoRow label={m.logLevel()}>
								<Badge variant="secondary">{data.logLevel}</Badge>
							</InfoRow>

							<InfoRow label={m.passwordAuthentication()}>
								<Badge variant={data.disableLocalAuth ? "destructive" : "outline"}>
									{data.disableLocalAuth ? m.enabled() : m.disabled()}
								</Badge>
							</InfoRow>

							<InfoRow label={m.trustedProxies()}>
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
									<span className="text-muted-foreground text-xs italic">{m.noneConfigured()}</span>
								)}
							</InfoRow>
						</div>
					</CardContent>
				</Card>
			)}
		</div>
	);
}
