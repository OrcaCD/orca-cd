import { MainContent } from "@/components/application/settings/main-content";
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
import { GitBranch, RefreshCw, Server, Settings, Shield } from "lucide-react";
import {
	Sidebar,
	SidebarContent,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarInset,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarProvider,
	SidebarTrigger,
	useSidebar,
} from "@/components/ui/sidebar";

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

const settingsSections = [
	{ id: "general", label: "General", icon: Server },
	{ id: "source", label: "Source", icon: GitBranch },
	{ id: "sync", label: "Sync Policy", icon: RefreshCw },
	{ id: "danger", label: "Danger Zone", icon: Shield },
];

function SettingsPage() {
	const { id } = Route.useParams();
	const [activeSection, setActiveSection] = useState("general");
	return (
		<div className="space-y-6">
			<div className="flex flex-col lg:flex-row gap-6">
				<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
					<Sidebar className="border-r md:top-14">
						<SidebarHeader className="px-4 py-5">
							<div className="flex items-center gap-2 font-semibold">
								<Settings className="size-4" />
								Settings
							</div>
						</SidebarHeader>
						<SidebarContent>
							<SidebarGroup>
								<SidebarGroupLabel>Application</SidebarGroupLabel>
								<SidebarGroupContent>
									<SidebarMenu>
										{settingsSections.map((item) => (
											<SettingsSidebarMenuItem
												key={item.id}
												item={item}
												activeSection={activeSection}
												setActiveSection={setActiveSection}
											/>
										))}
									</SidebarMenu>
								</SidebarGroupContent>
							</SidebarGroup>
						</SidebarContent>
					</Sidebar>
					<SidebarInset className="min-w-0">
						<div className="w-full space-y-6 overflow-y-auto p-4 sm:p-6">
							<div className="flex items-center gap-2 md:hidden">
								<SidebarTrigger className="-ml-1" />
								<span className="font-semibold">Settings</span>
							</div>
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

							<MainContent activeSection={activeSection} />
						</div>
					</SidebarInset>
				</SidebarProvider>
			</div>
		</div>
	);
}

function SettingsSidebarMenuItem({
	item,
	activeSection,
	setActiveSection,
}: {
	item: (typeof settingsSections)[number];
	activeSection: string;
	setActiveSection: (id: string) => void;
}) {
	const { isMobile, setOpenMobile } = useSidebar();
	const isActive = activeSection === item.id;

	return (
		<SidebarMenuItem className="mb-2">
			<SidebarMenuButton asChild isActive={isActive}>
				<div
					onClick={() => {
						setActiveSection(item.id);
						if (isMobile) {
							setOpenMobile(false);
						}
					}}
				>
					<item.icon />
					<span>{item.label}</span>
				</div>
			</SidebarMenuButton>
		</SidebarMenuItem>
	);
}
