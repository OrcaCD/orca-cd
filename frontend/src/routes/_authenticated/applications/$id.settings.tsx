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
import { GitBranch, RefreshCw, Server, Settings, Shield, Save, Trash2 } from "lucide-react";
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
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";

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

	const [autoSync, setAutoSync] = useState(true);
	const [selfHeal, setSelfHeal] = useState(true);
	const [pruneResources, setPruneResources] = useState(false);
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

							<div className="flex-1 space-y-6">
								<div>
									{activeSection === "general" && (
										<div className="space-y-6">
											<div className="bg-card border border-border rounded-lg p-6 space-y-6">
												<h2 className="text-lg font-semibold">General Settings</h2>

												<div className="space-y-4">
													<div className="space-y-2">
														<Label htmlFor="name">Application Name</Label>
														<Input
															id="name"
															defaultValue="api-gateway"
															className="bg-muted border-border"
														/>
													</div>
												</div>

												<Button>
													<Save className="mr-2 h-4 w-4" />
													Save Changes
												</Button>
											</div>
										</div>
									)}
								</div>

								<div>
									{activeSection === "source" && (
										<div className="space-y-6">
											<div className="bg-card border border-border rounded-lg p-6 space-y-6">
												<h2 className="text-lg font-semibold">Source Repository</h2>

												<div className="space-y-4">
													<div className="space-y-2">
														<Label htmlFor="repo">Repository URL</Label>
														<Input
															id="repo"
															defaultValue="https://github.com/org/api-gateway"
															className="bg-muted border-border"
														/>
													</div>

													<div className="space-y-2">
														<Label htmlFor="branch">Target Branch</Label>
														<Input
															id="branch"
															defaultValue="main"
															className="bg-muted border-border"
														/>
													</div>

													<div className="space-y-2">
														<Label htmlFor="path">Compose File Path</Label>
														<Input
															id="path"
															defaultValue="/docker-compose.yml"
															className="bg-muted border-border"
														/>
													</div>
												</div>

												<Button>
													<Save className="mr-2 h-4 w-4" />
													Save Changes
												</Button>
											</div>

											<div className="bg-card border border-border rounded-lg p-6 space-y-6">
												<h2 className="text-lg font-semibold">Target Host</h2>

												<div className="space-y-4">
													<div className="space-y-2">
														<Label htmlFor="host">Host</Label>
														<Select defaultValue="prod-server-01">
															<SelectTrigger className="bg-muted border-border">
																<SelectValue />
															</SelectTrigger>
															<SelectContent>
																<SelectItem value="prod-server-01">prod-server-01</SelectItem>
																<SelectItem value="prod-server-02">prod-server-02</SelectItem>
																<SelectItem value="staging-server">staging-server</SelectItem>
															</SelectContent>
														</Select>
													</div>

													<div className="space-y-2">
														<Label htmlFor="deployPath">Deployment Path</Label>
														<Input
															id="deployPath"
															defaultValue="/opt/apps/api-gateway"
															className="bg-muted border-border"
														/>
													</div>
												</div>

												<Button>
													<Save className="mr-2 h-4 w-4" />
													Save Changes
												</Button>
											</div>
										</div>
									)}
								</div>

								<div>
									{activeSection === "sync" && (
										<div className="space-y-6">
											<div className="bg-card border border-border rounded-lg p-6 space-y-6">
												<h2 className="text-lg font-semibold">Sync Policy</h2>

												<div className="space-y-6">
													<div className="flex items-center justify-between">
														<div className="space-y-1">
															<Label>Auto-Sync</Label>
															<p className="text-sm text-muted-foreground">
																Automatically sync when changes are detected
															</p>
														</div>
														<Switch checked={autoSync} onCheckedChange={setAutoSync} />
													</div>

													<div className="flex items-center justify-between">
														<div className="space-y-1">
															<Label>Self-Heal</Label>
															<p className="text-sm text-muted-foreground">
																Automatically correct drift from desired state
															</p>
														</div>
														<Switch checked={selfHeal} onCheckedChange={setSelfHeal} />
													</div>

													<div className="flex items-center justify-between">
														<div className="space-y-1">
															<Label>Prune Resources</Label>
															<p className="text-sm text-muted-foreground">
																Remove containers not defined in manifest
															</p>
														</div>
														<Switch checked={pruneResources} onCheckedChange={setPruneResources} />
													</div>
												</div>

												<Button>
													<Save className="mr-2 h-4 w-4" />
													Save Changes
												</Button>
											</div>
										</div>
									)}
								</div>

								<div>
									{activeSection === "danger" && (
										<div className="space-y-6">
											<div className="bg-card border border-destructive/50 rounded-lg p-6 space-y-6">
												<h2 className="text-lg font-semibold text-destructive">Danger Zone</h2>

												<div className="space-y-4">
													<div className="flex items-center justify-between p-4 bg-muted rounded-lg">
														<div>
															<p className="font-medium">Delete Application</p>
															<p className="text-sm text-muted-foreground">
																Permanently delete this application and all its data
															</p>
														</div>
														<Button variant="destructive">
															<Trash2 className="mr-2 h-4 w-4" />
															Delete
														</Button>
													</div>
												</div>
											</div>
										</div>
									)}
								</div>
							</div>
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
