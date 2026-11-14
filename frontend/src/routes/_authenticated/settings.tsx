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
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Key, User } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import { BreadcrumbItem, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";

export const Route = createFileRoute("/_authenticated/settings")({
	component: SettingsLayout,
});

const settingsPages = [
	{
		title: () => m.profile(),
		icon: User,
		path: "/settings/profile",
	},
	{
		title: () => m.security(),
		icon: Key,
		path: "/settings/security",
	},
];

function SettingsLayout() {
	const pathname = useRouterState({ select: (state) => state.location.pathname });

	return (
		<SidebarLayout
			sidebar={
				<SidebarGroup>
					<SidebarGroupLabel>{m.userSettings()}</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu>
							{settingsPages.map((item) => (
								<SettingsSidebarMenuItem key={item.path} item={item} pathname={pathname} />
							))}
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>
			}
			breadcrumb={
				<>
					<BreadcrumbItem>
						<BreadcrumbPage>{m.settings()}</BreadcrumbPage>
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<BreadcrumbPage>
							{settingsPages.find((page) => page.path === pathname)?.title()}
						</BreadcrumbPage>
					</BreadcrumbItem>
				</>
			}
		>
			<Outlet />
		</SidebarLayout>
	);
}

function SettingsSidebarMenuItem({
	item,
	pathname,
}: {
	item: (typeof settingsPages)[number];
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
