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
import { createFileRoute, Link, Outlet, redirect, useRouterState } from "@tanstack/react-router";
import { IdCard, Info, Shield, Users } from "lucide-react";

export const Route = createFileRoute("/_authenticated/admin")({
	beforeLoad: ({ context }) => {
		if (!context.auth.isAdmin) {
			throw redirect({ to: "/" });
		}
	},
	component: AdminLayout,
});

const adminPages = [
	{
		title: "System Info",
		icon: Info,
		path: "/admin/system-info",
	},
	{
		title: "OIDC Providers",
		icon: IdCard,
		path: "/admin/oidc-providers",
	},
	{
		title: "User Management",
		icon: Users,
		path: "/admin/users",
	},
];

function AdminLayout() {
	const { location } = useRouterState();

	return (
		<SidebarProvider>
			<Sidebar collapsible="none" className="h-screen border-r">
				<SidebarHeader className="px-4 py-5">
					<div className="flex items-center gap-2 font-semibold">
						<Shield className="size-4" />
						Admin
					</div>
				</SidebarHeader>
				<SidebarContent>
					<SidebarGroup>
						<SidebarGroupLabel>Management</SidebarGroupLabel>
						<SidebarGroupContent>
							<SidebarMenu>
								{adminPages.map((item) => (
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
