import {
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	useSidebar,
} from "@/components/ui/sidebar";
import { SidebarLayout, sidebarMenuButtonClass } from "@/components/sidebar-layout";
import { createFileRoute, Link, Outlet, redirect, useRouterState } from "@tanstack/react-router";
import { IdCard, Info, Logs, Users } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import { BreadcrumbItem, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";

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
		title: () => m.adminSystemInfo(),
		icon: Info,
		path: "/admin/system-info",
	},
	{
		title: () => m.adminOidcProviders(),
		icon: IdCard,
		path: "/admin/oidc-providers",
	},
	{
		title: () => m.adminUserManagement(),
		icon: Users,
		path: "/admin/users",
	},
	{
		title: () => m.adminAuditLog(),
		icon: Logs,
		path: "/admin/audit-log",
	},
];

function AdminLayout() {
	const { location } = useRouterState();

	return (
		<SidebarLayout
			sidebar={
				<SidebarGroup>
					<SidebarGroupLabel>{m.admin()}</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu>
							{adminPages.map((item) => (
								<AdminSidebarMenuItem key={item.path} item={item} pathname={location.pathname} />
							))}
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>
			}
			breadcrumb={
				<>
					<BreadcrumbItem>
						<BreadcrumbPage>{m.admin()}</BreadcrumbPage>
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<BreadcrumbPage>
							{adminPages.find((page) => page.path === location.pathname)?.title()}
						</BreadcrumbPage>
					</BreadcrumbItem>
				</>
			}
		>
			<Outlet />
		</SidebarLayout>
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
		<SidebarMenuItem className="mb-0.5">
			<SidebarMenuButton
				isActive={isActive}
				className={sidebarMenuButtonClass}
				render={
					<Link
						to={item.path}
						onClick={() => {
							if (isMobile) {
								setOpenMobile(false);
							}
						}}
					>
						<item.icon />
						<span>{item.title()}</span>
					</Link>
				}
			></SidebarMenuButton>
		</SidebarMenuItem>
	);
}
