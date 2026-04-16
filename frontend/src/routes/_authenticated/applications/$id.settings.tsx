import { MainContent } from "@/components/application/settings/main-content";
import { SidebarNavigation } from "@/components/application/settings/sidebar-navigation";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import {
	Breadcrumb,
	BreadcrumbItem,
	BreadcrumbLink,
	BreadcrumbList,
	BreadcrumbPage,
	BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";

export const Route = createFileRoute("/_authenticated/applications/$id/settings")({
	component: SettingsPage,
	head: () => ({
		meta: [
			{
				title: "Settings",
			},
		],
	}),
});

function SettingsPage() {
	const { id } = Route.useParams();
	const [activeSection, setActiveSection] = useState("general");
	return (
		<div className="p-6 space-y-6">
			<Breadcrumb>
				<BreadcrumbList>
					<BreadcrumbItem>
						<BreadcrumbLink asChild>
							<Link to="/applications">Applications</Link>
						</BreadcrumbLink>
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<BreadcrumbLink asChild>
							<Link to="/applications/$id" params={{ id: id }}>
								api-gateway
							</Link>
						</BreadcrumbLink>
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<BreadcrumbPage>Settings</BreadcrumbPage>
					</BreadcrumbItem>
				</BreadcrumbList>
			</Breadcrumb>
			<div className="flex flex-col lg:flex-row gap-6">
				<SidebarNavigation activeSection={activeSection} setActiveSection={setActiveSection} />
				<MainContent activeSection={activeSection} />
			</div>
		</div>
	);
}
