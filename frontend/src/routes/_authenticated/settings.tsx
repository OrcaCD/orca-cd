import {
	Sidebar,
	SidebarContent,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarProvider,
} from "@/components/ui/sidebar";
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Key, Settings, User } from "lucide-react";

export const Route = createFileRoute("/_authenticated/settings")({
	component: SettingsLayout,
});

const settingsPages = [
	{
		title: "Profile",
		icon: User,
		path: "/settings/profile",
	},
	{
		title: "Security",
		icon: Key,
		path: "/settings/security",
	},
];

function SettingsLayout() {
	const { location } = useRouterState();

	return (
		<SidebarProvider>
			<Sidebar collapsible="none" className="h-screen border-r">
				<SidebarHeader className="px-4 py-5">
					<div className="flex items-center gap-2 font-semibold">
						<Settings className="size-4" />
						Settings
					</div>
				</SidebarHeader>
				<SidebarContent>
					<SidebarGroup>
						<SidebarGroupLabel>Account</SidebarGroupLabel>
						<SidebarGroupContent>
							<SidebarMenu>
								{settingsPages.map((item) => (
									<SidebarMenuItem key={item.title}>
										<SidebarMenuButton asChild isActive={location.pathname === item.path}>
											<Link to={item.path}>
												<item.icon />
												<span>{item.title}</span>
											</Link>
										</SidebarMenuButton>
									</SidebarMenuItem>
								))}
							</SidebarMenu>
						</SidebarGroupContent>
					</SidebarGroup>
				</SidebarContent>
			</Sidebar>
			<div className="p-6 space-y-6 w-full overflow-y-auto">
				<Outlet />
			</div>
		</SidebarProvider>
	);
}
