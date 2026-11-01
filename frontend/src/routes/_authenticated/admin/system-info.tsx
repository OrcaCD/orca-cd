import { createFileRoute } from "@tanstack/react-router";
import type { ReactNode } from "react";
import {
	CircleCheck,
	Clock,
	ExternalLink,
	GitCommitHorizontal,
	Globe,
	Info,
	ScrollText,
	ShieldCheck,
	Tag,
	TriangleAlert,
	type LucideIcon,
} from "lucide-react";
import CopyButton from "@/components/copy-btn";
import { RelativeTime } from "@/components/relative-time";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

interface SystemInfo {
	host: string;
	port: string;
	logLevel: string;
	trustedProxies: string[];
	allowedIps: string[];
	appUrl: string;
	disableLocalAuth: boolean;
	version: string;
	commit: string;
	buildDate: string;
}

interface GitHubRelease {
	tag_name: string;
}

const repoUrl = "https://github.com/OrcaCD/orca-cd";
const commitHashPattern = /^[0-9a-f]{7,40}$/i;
const logLevels = ["trace", "debug", "info", "warn", "error"];

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

function formatListenAddress(host: string, port: string) {
	const resolvedHost = host || "0.0.0.0";
	return `${resolvedHost.includes(":") ? `[${resolvedHost}]` : resolvedHost}:${port}`;
}

function Mono({ children }: { children: ReactNode }) {
	return (
		<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs break-all">{children}</code>
	);
}

function MonoList({ values }: { values: string[] }) {
	if (!values || values.length === 0) {
		return <span className="text-muted-foreground text-xs italic">{m.noneConfigured()}</span>;
	}
	return values.map((value) => <Mono key={value}>{value}</Mono>);
}

function SettingsSection({
	title,
	icon: Icon,
	children,
}: {
	title: string;
	icon: LucideIcon;
	children: ReactNode;
}) {
	return (
		<Card>
			<CardHeader>
				<CardTitle className="flex items-center gap-2">
					<Icon className="text-muted-foreground size-4" aria-hidden="true" />
					{title}
				</CardTitle>
			</CardHeader>
			<CardContent className="divide-border divide-y">{children}</CardContent>
		</Card>
	);
}

function SettingRow({
	label,
	envVar,
	hint,
	children,
}: {
	label: string;
	envVar: string;
	hint?: string;
	children: ReactNode;
}) {
	return (
		<div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-1.5 py-3 first:pt-0 last:pb-0">
			<div className="min-w-0">
				<div>{label}</div>
				<code className="text-muted-foreground font-mono text-[11px]">{envVar}</code>
			</div>
			<div className="flex min-w-0 flex-col items-end gap-1 text-right">
				<div className="flex min-w-0 flex-wrap items-center justify-end gap-1.5">{children}</div>
				{hint && <span className="text-muted-foreground text-xs">{hint}</span>}
			</div>
		</div>
	);
}

function LogLevelScale({ level }: { level: string }) {
	const current = level.toLowerCase();
	if (!logLevels.includes(current)) {
		return <Badge variant="secondary">{level}</Badge>;
	}

	return (
		<div className="bg-muted flex gap-0.5 rounded-md p-0.5">
			{logLevels.map((logLevel) => (
				<span
					key={logLevel}
					aria-hidden={logLevel !== current}
					className={cn(
						"rounded px-1.5 py-0.5 text-xs",
						logLevel === current
							? "bg-card text-foreground ring-border shadow-sm ring-1"
							: "text-muted-foreground",
					)}
				>
					{logLevel}
				</span>
			))}
		</div>
	);
}

function CommitInfo({ commit }: { commit: string }) {
	if (!commitHashPattern.test(commit)) {
		return (
			<span className="flex items-center gap-1.5">
				<GitCommitHorizontal className="size-3.5" aria-hidden="true" />
				<span className="font-mono">{commit || m.unknown()}</span>
			</span>
		);
	}

	return (
		<span className="flex items-center gap-1">
			<GitCommitHorizontal className="size-3.5" aria-hidden="true" />
			<a
				href={`${repoUrl}/commit/${commit}`}
				target="_blank"
				rel="noopener noreferrer"
				className="text-foreground font-mono hover:underline"
			>
				{commit.slice(0, 7)}
			</a>
			<CopyButton text={commit} title={m.copyCommitHash()} />
		</span>
	);
}

function ReleaseStatus({ version }: { version: string }) {
	const { data } = useFetch<GitHubRelease>(
		"https://api.github.com/repos/OrcaCD/orca-cd/releases/latest",
	);

	if (!data) {
		return null;
	}

	const isRelease = version.startsWith("v");
	const isUpToDate = data.tag_name === version;
	const releaseNotesLink = (
		<a
			href={`${repoUrl}/releases/tag/${data.tag_name}`}
			target="_blank"
			rel="noopener noreferrer"
			className="text-primary inline-flex items-center gap-1 hover:underline"
		>
			{m.releaseNotes()}
			<ExternalLink className="size-3" aria-hidden="true" />
		</a>
	);

	let icon = <Tag className="text-primary size-4 shrink-0" aria-hidden="true" />;
	let title = m.latestRelease({ version: data.tag_name });
	let details: ReactNode = releaseNotesLink;

	if (isRelease) {
		icon = isUpToDate ? (
			<CircleCheck className="size-4 shrink-0 text-green-500" aria-hidden="true" />
		) : (
			<TriangleAlert className="size-4 shrink-0 text-amber-500" aria-hidden="true" />
		);
		title = isUpToDate ? m.upToDate() : m.updateAvailable();
		details = (
			<>
				{m.latestRelease({ version: data.tag_name })} · {releaseNotesLink}
			</>
		);
	}

	return (
		<div className="bg-muted/50 flex items-center gap-2.5 rounded-lg px-3 py-2">
			{icon}
			<div className="flex flex-col">
				<span className="font-medium">{title}</span>
				<span className="text-muted-foreground text-xs">{details}</span>
			</div>
		</div>
	);
}

function BuildInfoCard({ systemInfo }: { systemInfo: SystemInfo }) {
	const isRelease = systemInfo.version.startsWith("v");

	return (
		<Card>
			<CardContent className="flex flex-wrap items-center justify-between gap-4">
				<div className="flex min-w-0 items-center gap-4">
					<img src="/assets/logo-dark.svg" alt="" className="size-11 shrink-0" />
					<div className="min-w-0">
						<div className="flex flex-wrap items-center gap-2">
							<h2 className="text-xl font-semibold tracking-tight">
								OrcaCD <span className="font-mono font-medium">{systemInfo.version}</span>
							</h2>
							{!isRelease && <Badge variant="secondary">{m.developmentBuild()}</Badge>}
						</div>
						<div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
							<CommitInfo commit={systemInfo.commit} />
							<span className="flex items-center gap-1.5">
								<Clock className="size-3.5" aria-hidden="true" />
								<RelativeTime
									value={systemInfo.buildDate}
									format={(time) => m.builtAt({ time })}
									fallback={m.unknown()}
								/>
							</span>
						</div>
					</div>
				</div>
				<ReleaseStatus version={systemInfo.version} />
			</CardContent>
		</Card>
	);
}

function SystemInfoPage() {
	const { data, isLoading } = useFetch<SystemInfo>("/admin/system-info");

	return (
		<div className="flex flex-col gap-6">
			{isLoading && <p className="text-muted-foreground text-sm">{m.loadingSystemInfo()}</p>}

			{data && (
				<>
					<BuildInfoCard systemInfo={data} />

					<div className="grid gap-6 lg:grid-cols-2">
						<SettingsSection title={m.network()} icon={Globe}>
							<SettingRow label={m.appUrl()} envVar="APP_URL">
								<Mono>{data.appUrl || "—"}</Mono>
							</SettingRow>
							<SettingRow label={m.listenAddress()} envVar="HOST · PORT">
								<Mono>{formatListenAddress(data.host, data.port)}</Mono>
							</SettingRow>
							<SettingRow label={m.trustedProxies()} envVar="TRUSTED_PROXIES">
								<MonoList values={data.trustedProxies} />
							</SettingRow>
						</SettingsSection>

						<div className="flex flex-col gap-6">
							<SettingsSection title={m.accessAndSecurity()} icon={ShieldCheck}>
								<SettingRow
									label={m.passwordAuthentication()}
									envVar="DISABLE_LOCAL_AUTH"
									hint={data.disableLocalAuth ? m.ssoOnlyHint() : undefined}
								>
									{data.disableLocalAuth ? (
										<Badge
											variant="outline"
											className="border-primary/30 bg-primary/10 text-primary"
										>
											{m.ssoOnly()}
										</Badge>
									) : (
										<Badge variant="secondary">{m.enabled()}</Badge>
									)}
								</SettingRow>
								<SettingRow
									label={m.allowedIps()}
									envVar="ALLOWED_IPS"
									hint={data.allowedIps?.length ? undefined : m.openToAllHint()}
								>
									{data.allowedIps?.length ? (
										<MonoList values={data.allowedIps} />
									) : (
										<Badge
											variant="outline"
											className="border-amber-500/30 bg-amber-500/20 text-amber-600 dark:text-amber-400"
										>
											{m.openToAll()}
										</Badge>
									)}
								</SettingRow>
							</SettingsSection>

							<SettingsSection title={m.diagnostics()} icon={ScrollText}>
								<SettingRow label={m.logLevel()} envVar="LOG_LEVEL">
									<LogLevelScale level={data.logLevel} />
								</SettingRow>
							</SettingsSection>
						</div>
					</div>

					<p className="text-muted-foreground flex items-start gap-1.5 text-xs">
						<Info className="mt-px size-3.5 shrink-0" aria-hidden="true" />
						{m.envConfigHint()}
					</p>
				</>
			)}
		</div>
	);
}
