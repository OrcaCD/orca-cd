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
		<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
			<Sidebar className="border-r md:top-14">
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
									<AdminSidebarMenuItem key={item.title} item={item} pathname={location.pathname} />
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
						<span className="font-semibold">Admin</span>
					</div>
					<Outlet />
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}

function AdminSidebarMenuItem({
	item,
	pathname,
}: {
	item: (typeof adminPages)[number];
	pathname: string;
}) {
	const { isMobile, setOpenMobile } = useSidebar();
	const isActive = pathname === item.path || pathname.startsWith(`${item.path}/`);

	return (
		<SidebarMenuItem className="mb-2">
			<SidebarMenuButton asChild isActive={isActive}>
				<Link
					to={item.path}
					onClick={() => {
						if (isMobile) {
							setOpenMobile(false);
						}
					}}
				>
					<item.icon />
					<span>{item.title}</span>
				</Link>
			</SidebarMenuButton>
		</SidebarMenuItem>
	);
}
