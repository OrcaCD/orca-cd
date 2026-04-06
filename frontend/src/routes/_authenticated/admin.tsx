import {
	Sidebar,
	SidebarContent,
	SidebarGroup,
	SidebarGroupContent,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarProvider,
} from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { IdCard, Info, Users } from "lucide-react";

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
	return (
		<SidebarProvider>
			<TooltipProvider>
				<Sidebar collapsible="none" className="h-screen w-sm">
					<SidebarContent>
						<SidebarGroup>
							<SidebarGroupContent>
								<SidebarMenu>
									{adminPages.map((item) => (
										<SidebarMenuItem key={item.title} className="mb-2">
											<SidebarMenuButton tooltip={item.title} asChild>
												<a href={item.path}>
													<item.icon />
													<span>{item.title}</span>
												</a>
											</SidebarMenuButton>
										</SidebarMenuItem>
									))}
								</SidebarMenu>
							</SidebarGroupContent>
						</SidebarGroup>
					</SidebarContent>
				</Sidebar>
				<div className="p-6 space-y-6 w-full">
					<Outlet />
				</div>
			</TooltipProvider>
		</SidebarProvider>
	);
}
