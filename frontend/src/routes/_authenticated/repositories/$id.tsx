import {
	Breadcrumb,
	BreadcrumbList,
	BreadcrumbItem,
	BreadcrumbLink,
	BreadcrumbSeparator,
	BreadcrumbPage,
} from "@/components/ui/breadcrumb";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { Separator } from "@/components/ui/separator";
import {
	Sidebar,
	SidebarProvider,
	SidebarContent,
	SidebarGroup,
	SidebarGroupLabel,
	SidebarGroupContent,
	SidebarMenu,
	SidebarInset,
	SidebarTrigger,
	useSidebar,
	SidebarMenuItem,
	SidebarMenuButton,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useFetch } from "@/lib/api";
import type { Repository } from "@/lib/repositories";
import { m } from "@/lib/paraglide/messages";
import { isApple } from "@/lib/utils";
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { KeyRound, RefreshCw } from "lucide-react";

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
		<div className="flex flex-col min-h-[calc(100svh-3.5rem)] w-full">
			<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
				<Sidebar collapsible="icon" className="border-r md:top-14">
					<SidebarContent>
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
										<BreadcrumbLink
											render={<Link to="/repositories">{m.pageRepositories()}</Link>}
										/>
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
								</BreadcrumbList>
							</Breadcrumb>
						</div>
						<Outlet />
					</div>
				</SidebarInset>
			</SidebarProvider>
		</div>
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
		<SidebarMenuItem className="mb-2">
			<SidebarMenuButton
				isActive={isActive}
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
