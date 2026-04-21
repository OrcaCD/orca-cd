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
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Key, Settings, User } from "lucide-react";
import { m } from "@/lib/paraglide/messages";

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
	const { location } = useRouterState();

	return (
		<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
			<Sidebar className="border-r md:top-14">
				<SidebarHeader className="px-4 py-5">
					<div className="flex items-center gap-2 font-semibold">
						<Settings className="size-4" />
						{m.settings()}
					</div>
				</SidebarHeader>
				<SidebarContent>
					<SidebarGroup>
						<SidebarGroupLabel>{m.account()}</SidebarGroupLabel>
						<SidebarGroupContent>
							<SidebarMenu>
								{settingsPages.map((item) => (
									<SettingsSidebarMenuItem
										key={item.path}
										item={item}
										pathname={location.pathname}
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
						<span className="font-semibold">{m.settings()}</span>
					</div>
					<Outlet />
				</div>
			</SidebarInset>
		</SidebarProvider>
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
					<span>{item.title()}</span>
				</Link>
			</SidebarMenuButton>
		</SidebarMenuItem>
	);
}
