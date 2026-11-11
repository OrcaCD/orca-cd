import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { useState } from "react";
import { InfoTooltip, InfoTooltipContent, InfoTooltipTrigger } from "@/components/info-tooltip";
import { RelativeTime } from "@/components/relative-time";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { toast } from "@/components/ui/toast";
import { useNow } from "@/hooks/use-now";
import { useFetch } from "@/lib/api";
import { updateServiceReleaseAge, type Application } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import {
	dayInSeconds,
	effectiveMinimumSeconds,
	formatReleaseAge,
	formatRemaining,
	ImageUpdateStatus,
	shortDigest,
	type GlobalReleaseAgeSettings,
	type ImageUpdate,
	type ImageUpdates,
} from "@/lib/release-age";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/_authenticated/applications/$id/image-updates")({
	component: ImageUpdatesPage,
	head: () => ({
		meta: [
			{
				title: `${m.pageApplications()} - ${m.upcomingUpdatesTitle()}`,
			},
		],
	}),
});

const presetAges = [0, 1, 3, 7, 14, 30].map((days) => days * dayInSeconds);
const inheritValue = "inherit";

function ImageUpdatesPage() {
	const { id } = Route.useParams();
	const { data: application, mutate: mutateApplication } = useFetch<Application>(
		`/applications/${id}`,
	);
	const { data: global } = useFetch<GlobalReleaseAgeSettings>("/settings/release-age");
	const { data: updates } = useFetch<ImageUpdates>(`/applications/${id}/image-updates`);

	const appAge = effectiveMinimumSeconds(application, global);
	const inherited = application?.releaseAge.minimumSeconds === null;

	return (
		<Card>
			<CardHeader>
				<div className="flex flex-wrap items-start justify-between gap-2">
					<div className="space-y-1.5">
						<CardTitle>{m.upcomingUpdatesTitle()}</CardTitle>
						<CardDescription>{m.upcomingUpdatesDescription()}</CardDescription>
					</div>
					{updates?.checkedAt && (
						<p className="text-muted-foreground text-sm">
							{m.upcomingUpdatesLastCheck()} <RelativeTime value={updates.checkedAt} />
						</p>
					)}
				</div>
				<p className="text-muted-foreground flex flex-wrap items-center gap-1 text-sm">
					{m.upcomingUpdatesPolicy({ age: formatReleaseAge(appAge) })}
					{inherited && <span>({m.upcomingUpdatesPolicyGlobal()})</span>}
					<Link
						to="/applications/$id/settings/image-polling"
						params={{ id }}
						className="inline-flex items-center gap-1 font-medium underline-offset-4 hover:text-foreground hover:underline"
					>
						{m.upcomingUpdatesChangePolicy()}
						<ArrowRight className="h-3 w-3" />
					</Link>
				</p>
			</CardHeader>
			<Separator />
			<CardContent>
				{!updates ? (
					<Skeleton className="h-32 w-full" />
				) : updates.images.length === 0 ? (
					<p className="text-muted-foreground py-6 text-center text-sm">
						{m.upcomingUpdatesEmpty()}
					</p>
				) : (
					<div className="overflow-x-auto">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>{m.upcomingUpdatesImage()}</TableHead>
									<TableHead>{m.upcomingUpdatesVersion()}</TableHead>
									<TableHead>{m.upcomingUpdatesPushed()}</TableHead>
									<TableHead>{m.upcomingUpdatesStatus()}</TableHead>
									<TableHead>{m.upcomingUpdatesMinimumAge()}</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{updates.images.map((image) => (
									<TableRow key={image.service}>
										<TableCell className="max-w-72">
											<div className="font-medium">{image.service}</div>
											<div
												className="text-muted-foreground truncate font-mono text-xs"
												title={image.image}
											>
												{image.image}
											</div>
										</TableCell>
										<TableCell>
											<VersionCell image={image} />
										</TableCell>
										<TableCell>
											<PushedCell image={image} />
										</TableCell>
										<TableCell>
											<StatusCell image={image} />
										</TableCell>
										<TableCell>
											<MinimumAgeSelect
												applicationId={id}
												image={image}
												override={application?.releaseAge.serviceOverrides[image.service]}
												appAge={appAge}
												onUpdated={(updated) => mutateApplication(updated, { revalidate: false })}
											/>
										</TableCell>
									</TableRow>
								))}
							</TableBody>
						</Table>
					</div>
				)}
			</CardContent>
		</Card>
	);
}

function versionLabel(version: string, digest: string): string {
	return version || shortDigest(digest) || "–";
}

function VersionCell({ image }: { image: ImageUpdate }) {
	const current = versionLabel(image.currentVersion, image.currentDigest);
	if (!image.newDigest) {
		return <span className="font-mono text-xs">{current}</span>;
	}
	return (
		<span className="inline-flex items-center gap-1 font-mono text-xs" title={image.newDigest}>
			<span className="text-muted-foreground">{current}</span>
			<ArrowRight className="h-3 w-3 shrink-0" />
			<span className="font-medium">{versionLabel(image.newVersion, image.newDigest)}</span>
		</span>
	);
}

function PushedCell({ image }: { image: ImageUpdate }) {
	if (!image.pushedAt) {
		return <span className="text-muted-foreground">–</span>;
	}
	return (
		<span className="inline-flex items-center gap-1 text-sm">
			<RelativeTime value={image.pushedAt} />
			{image.firstSeen && (
				<InfoTooltip>
					<InfoTooltipTrigger className="text-muted-foreground cursor-help text-xs underline decoration-dotted">
						{m.upcomingUpdatesFirstSeen()}
					</InfoTooltipTrigger>
					<InfoTooltipContent>{m.upcomingUpdatesFirstSeenDescription()}</InfoTooltipContent>
				</InfoTooltip>
			)}
		</span>
	);
}

function StatusCell({ image }: { image: ImageUpdate }) {
	const now = useNow();

	switch (image.status) {
		case ImageUpdateStatus.Waiting: {
			const pushed = image.pushedAt ? new Date(image.pushedAt).getTime() : now;
			const eligible = image.eligibleAt ? new Date(image.eligibleAt).getTime() : now;
			const remaining = eligible - now;
			const progress =
				eligible > pushed ? Math.min(1, Math.max(0, (now - pushed) / (eligible - pushed))) : 1;
			return (
				<div className="w-44 space-y-1.5" title={image.reason}>
					<Badge
						variant="outline"
						className="border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400"
					>
						{remaining > 0
							? m.imageUpdateStatusWaiting({ remaining: formatRemaining(remaining) })
							: m.imageUpdateStatusWaitingDue()}
					</Badge>
					<div
						className="h-1.5 overflow-hidden rounded-full bg-muted"
						role="progressbar"
						aria-valuemin={0}
						aria-valuemax={100}
						aria-valuenow={Math.round(progress * 100)}
					>
						<div
							className="h-full rounded-full bg-amber-500"
							style={{ width: `${progress * 100}%` }}
						/>
					</div>
				</div>
			);
		}
		case ImageUpdateStatus.Updated:
			return <Badge variant="success">{m.imageUpdateStatusUpdated()}</Badge>;
		case ImageUpdateStatus.Blocked:
		case ImageUpdateStatus.Failed:
			return (
				<div className="max-w-64 space-y-1">
					<Badge variant="destructive">
						{image.status === ImageUpdateStatus.Blocked
							? m.imageUpdateStatusBlocked()
							: m.imageUpdateStatusFailed()}
					</Badge>
					{image.reason && (
						<p className="text-muted-foreground line-clamp-2 text-xs" title={image.reason}>
							{image.reason}
						</p>
					)}
				</div>
			);
		default:
			return <Badge variant="outline">{m.imageUpdateStatusUpToDate()}</Badge>;
	}
}

function MinimumAgeSelect({
	applicationId,
	image,
	override,
	appAge,
	onUpdated,
}: {
	applicationId: string;
	image: ImageUpdate;
	override: number | undefined;
	appAge: number;
	onUpdated: (application: Application) => Promise<unknown>;
}) {
	const [saving, setSaving] = useState(false);

	// The compose label wins over everything set in the UI.
	if (image.ageSource === "label") {
		return (
			<span className="text-sm" title={m.releaseAgeSetByLabel()}>
				{formatReleaseAge(image.minimumAgeSeconds)}{" "}
				<span className="text-muted-foreground text-xs">({m.releaseAgeLabelShort()})</span>
			</span>
		);
	}

	const ages =
		override === undefined || presetAges.includes(override)
			? presetAges
			: [...presetAges, override];
	const items = [
		{ value: inheritValue, label: m.releaseAgeInherit({ age: formatReleaseAge(appAge) }) },
		...ages.map((seconds) => ({ value: String(seconds), label: formatReleaseAge(seconds) })),
	];

	async function change(value: string | null) {
		if (value === null) {
			return;
		}
		setSaving(true);
		try {
			const updated = await updateServiceReleaseAge(
				applicationId,
				image.service,
				value === inheritValue ? null : Number(value),
			);
			await onUpdated(updated);
			toast.add({ type: "success", title: m.releaseAgeServiceUpdated() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedSaveApplication(),
			});
		} finally {
			setSaving(false);
		}
	}

	return (
		<Select
			items={items}
			value={override === undefined ? inheritValue : String(override)}
			onValueChange={change}
			disabled={saving}
		>
			<SelectTrigger
				className={cn("w-44", override === undefined && "text-muted-foreground")}
				aria-label={m.releaseAgeServiceLabel({ service: image.service })}
			>
				<SelectValue />
			</SelectTrigger>
			<SelectContent>
				{items.map((item) => (
					<SelectItem key={item.value} value={item.value}>
						{item.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}
