import {
	BreadcrumbItem,
	BreadcrumbLink,
	BreadcrumbSeparator,
	BreadcrumbPage,
} from "@/components/ui/breadcrumb";
import {
	SidebarGroup,
	SidebarGroupLabel,
	SidebarGroupContent,
	SidebarMenu,
	useSidebar,
	SidebarMenuItem,
	SidebarMenuButton,
} from "@/components/ui/sidebar";
import { SidebarLayout, sidebarMenuButtonClass } from "@/components/sidebar-layout";
import { useFetch } from "@/lib/api";
import type { Application } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Activity, History, Info, Settings, ImageDown, Braces, RotateCcw } from "lucide-react";

export const Route = createFileRoute("/_authenticated/applications/$id")({
	component: ApplicationsLayout,
});

type SidebarGroup = {
	title: () => string;
	children: {
		title: () => string;
		icon?: any;
		to: string;
	}[];
};

const sidebarGroups: SidebarGroup[] = [
	{
		title: () => m.management(),
		children: [
			{
				title: () => m.details(),
				icon: Info,
				to: "/applications/$id/details",
			},
			{
				title: () => m.healthTitle(),
				icon: Activity,
				to: "/applications/$id/health",
			},
			{
				title: () => m.applicationHistory(),
				icon: History,
				to: "/applications/$id/history",
			},
		],
	},
	{
		title: () => m.settings(),
		children: [
			{
				title: () => m.general(),
				icon: Settings,
				to: "/applications/$id/settings/general",
			},
			{
				title: () => m.imageUpdatesSectionTitle(),
				icon: ImageDown,
				to: "/applications/$id/settings/image-polling",
			},
			{
				title: () => m.envTitle(),
				icon: Braces,
				to: "/applications/$id/settings/environment",
			},
			{
				title: () => m.rollbackSettingsTitle(),
				icon: RotateCcw,
				to: "/applications/$id/settings/rollback",
			},
		],
	},
];

function ApplicationsLayout() {
	const { id } = Route.useParams();
	const { data: application } = useFetch<Application>(`/applications/${id}`);
	const { location } = useRouterState();

	return (
		<SidebarLayout
			sidebar={sidebarGroups.map((group) => (
				<SidebarGroup key={group.title()}>
					<SidebarGroupLabel>{group.title()}</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu>
							{group.children.map((child) => (
								<SidebarItem key={child.to} item={child} pathname={location.pathname} id={id} />
							))}
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>
			))}
			breadcrumb={
				<>
					<BreadcrumbItem>
						<BreadcrumbLink
							render={<Link to="/applications">{m.pageApplications()}</Link>}
						></BreadcrumbLink>
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					{location.pathname.includes("/settings") ||
					location.pathname.includes("/history") ||
					location.pathname.includes("/health") ? (
						<>
							<BreadcrumbItem>
								<BreadcrumbLink
									render={
										<Link to="/applications/$id/details" params={{ id }}>
											{application?.name}
										</Link>
									}
								></BreadcrumbLink>
							</BreadcrumbItem>
							<BreadcrumbSeparator />
							<BreadcrumbItem>
								<BreadcrumbPage>
									{location.pathname.includes("/history")
										? m.applicationHistory()
										: location.pathname.includes("/health")
											? m.healthTitle()
											: m.settings()}
								</BreadcrumbPage>
							</BreadcrumbItem>
						</>
					) : (
						<BreadcrumbItem>
							<BreadcrumbPage>{application?.name}</BreadcrumbPage>
						</BreadcrumbItem>
					)}
				</>
			}
		>
			<Outlet />
		</SidebarLayout>
	);
}

function SidebarItem({
	item,
	pathname,
	id,
}: {
	item: {
		title: () => string;
		icon?: any;
		to: string;
	};
	pathname: string;
	id?: string;
}) {
	const { isMobile, setOpenMobile } = useSidebar();

	const targetPath = item.to.replace("$id", id ?? "");
	const isActive = pathname === targetPath || pathname.startsWith(`${targetPath}/`);

	const Icon = item.icon;

	return (
		<SidebarMenuItem className="mb-0.5">
			<SidebarMenuButton
				isActive={isActive}
				className={sidebarMenuButtonClass}
				render={
					<Link
						to={item.to}
						params={{ id: id ?? "" }}
						onClick={() => isMobile && setOpenMobile(false)}
					>
						{Icon && <Icon className="h-4 w-4" />}
						<span>{item.title()}</span>
					</Link>
				}
			></SidebarMenuButton>
		</SidebarMenuItem>
	);
}
