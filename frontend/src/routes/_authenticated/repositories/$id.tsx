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
import type { Repository } from "@/lib/repositories";
import { m } from "@/lib/paraglide/messages";
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { FileKey, KeyRound, RefreshCw } from "lucide-react";

export const Route = createFileRoute("/_authenticated/repositories/$id")({
	component: RepositorySettingsLayout,
});

const sidebarItems = [
	{
		title: () => m.editRepositoryAuthShort(),
		icon: KeyRound,
		to: "/repositories/$id/settings/auth",
	},
	{
		title: () => m.editRepositorySyncShort(),
		icon: RefreshCw,
		to: "/repositories/$id/settings/sync",
	},
	{
		title: () => m.editRepositorySigningShort(),
		icon: FileKey,
		to: "/repositories/$id/settings/signing",
	},
];

function RepositorySettingsLayout() {
	const { id } = Route.useParams();
	const { data: repository } = useFetch<Repository>(`/repositories/${id}`);
	const { location } = useRouterState();

	const currentPage = sidebarItems.find((item) => {
		const path = item.to.replace("$id", id);
		return location.pathname === path || location.pathname.startsWith(`${path}/`);
	});

	return (
		<SidebarLayout
			sidebar={
				<SidebarGroup>
					<SidebarGroupLabel>{m.repositorySettings()}</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu>
							{sidebarItems.map((item) => (
								<SidebarItem key={item.to} item={item} pathname={location.pathname} id={id} />
							))}
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>
			}
			breadcrumb={
				<>
					<BreadcrumbItem>
						<BreadcrumbLink render={<Link to="/repositories">{m.pageRepositories()}</Link>} />
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<BreadcrumbPage>{repository?.name ?? id}</BreadcrumbPage>
					</BreadcrumbItem>
					{currentPage && (
						<>
							<BreadcrumbSeparator />
							<BreadcrumbItem>
								<BreadcrumbPage>{currentPage.title()}</BreadcrumbPage>
							</BreadcrumbItem>
						</>
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
	item: (typeof sidebarItems)[number];
	pathname: string;
	id: string;
}) {
	const { isMobile, setOpenMobile } = useSidebar();

	const targetPath = item.to.replace("$id", id);
	const isActive = pathname === targetPath || pathname.startsWith(`${targetPath}/`);

	const Icon = item.icon;

	return (
		<SidebarMenuItem className="mb-0.5">
			<SidebarMenuButton
				isActive={isActive}
				className={sidebarMenuButtonClass}
				render={
					<Link to={item.to} params={{ id }} onClick={() => isMobile && setOpenMobile(false)}>
						{Icon && <Icon className="h-4 w-4" />}
						<span>{item.title()}</span>
					</Link>
				}
			/>
		</SidebarMenuItem>
	);
}
