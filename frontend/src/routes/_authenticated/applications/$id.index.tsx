import { Breadcrumb } from "@/components/application/details/breadcrumb";
import { Header } from "@/components/application/details/header";
import { InfoCards } from "@/components/application/details/info-cards";
import { Properties } from "@/components/application/details/properties";
import { HealthStatus, SyncStatus, type Application } from "@/lib/applications";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/applications/$id/")({
	component: ApplicationDetailsPage,
	head: () => ({
		meta: [
			{
				title: "Applications",
			},
		],
	}),
});

const mockApp: Application = {
	id: "1",
	name: "api-gateway",
	syncStatus: SyncStatus.Synced,
	healthStatus: HealthStatus.Healthy,
	repo: "github.com/org/api-gateway",
	branch: "main",
	commit: "a3f2b1c",
	commitMessage: "fix: update rate limiting configuration",
	lastSync: "2 minutes ago",
	path: "/docker-compose.yml",
	agent: "prod-server-01",
};

export default function ApplicationDetailsPage() {
	const { id } = Route.useParams();
	return (
		<div className="p-6 space-y-6">
			<Breadcrumb app={mockApp} />
			<Header app={mockApp} id={id} />
			<InfoCards app={mockApp} />
			<Properties />
		</div>
	);
}
