import {
	Sidebar,
	SidebarContent,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarInset,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarProvider,
	SidebarTrigger,
	useSidebar,
} from "@/components/ui/sidebar";
import { createFileRoute, Link, Outlet, redirect, useRouterState } from "@tanstack/react-router";
import { IdCard, Info, Logs, Users } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import { Separator } from "@/components/ui/separator";
import {
	Breadcrumb,
	BreadcrumbItem,
	BreadcrumbList,
	BreadcrumbPage,
	BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Kbd, KbdGroup } from "@/components/ui/kbd";

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
	const isApple = /Mac|iPod|iPhone|iPad/.test(navigator.platform);

	return (
		<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
			<Sidebar collapsible="icon" className="border-r md:top-14">
				<SidebarContent>
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
				</SidebarContent>
			</Sidebar>
			<SidebarInset className="min-w-0">
				<div className="w-full space-y-6 overflow-y-auto p-4 sm:p-6">
					<div className="flex items-center gap-2">
						<Tooltip>
							<TooltipTrigger render={<SidebarTrigger className="-ml-1" />} />
							<TooltipContent>
								<p>{m.toggleSidebar()}</p>

								<KbdGroup>
									<Kbd>{isApple ? "⌘" : "Ctrl"}</Kbd>
									<Kbd>B</Kbd>
								</KbdGroup>
							</TooltipContent>
						</Tooltip>

						<Separator
							orientation="vertical"
							className="mr-2 my-auto data-[orientation=vertical]:h-4"
						/>
						<Breadcrumb>
							<BreadcrumbList>
								<BreadcrumbItem>
									<BreadcrumbPage>{m.admin()}</BreadcrumbPage>
								</BreadcrumbItem>
								<BreadcrumbSeparator />
								<BreadcrumbItem>
									<BreadcrumbPage>
										{adminPages.find((page) => page.path === location.pathname)?.title()}
									</BreadcrumbPage>
								</BreadcrumbItem>
							</BreadcrumbList>
						</Breadcrumb>
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
			<SidebarMenuButton
				isActive={isActive}
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
