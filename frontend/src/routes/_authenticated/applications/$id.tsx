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
import type { Application } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Info, Settings, RefreshCw } from "lucide-react";

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
				title: () => m.imagePollSectionTitle(),
				icon: RefreshCw,
				to: "/applications/$id/settings/image-polling",
			},
		],
	},
];

function ApplicationsLayout() {
	const { id } = Route.useParams();
	const { data: application } = useFetch<Application>(`/applications/${id}`);
	const { location } = useRouterState();
	const isApple = /Mac|iPod|iPhone|iPad/.test(navigator.platform);

	return (
		<div className="flex flex-col min-h-[calc(100svh-3.5rem)] w-full">
			<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
				<Sidebar collapsible="icon" className="border-r md:top-14">
					<SidebarContent>
						{sidebarGroups.map((group) => (
							<SidebarGroup key={group.title()}>
								<SidebarGroupLabel>{group.title()}</SidebarGroupLabel>
								<SidebarGroupContent>
									<SidebarMenu>
										{group.children.map((child) => (
											<SidebarItem
												key={child.to}
												item={child}
												pathname={location.pathname}
												id={id}
											/>
										))}
									</SidebarMenu>
								</SidebarGroupContent>
							</SidebarGroup>
						))}
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
											render={<Link to="/applications">{m.pageApplications()}</Link>}
										></BreadcrumbLink>
									</BreadcrumbItem>
									<BreadcrumbSeparator />
									{location.pathname.includes("/settings") ? (
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
												<BreadcrumbPage>{m.settings()}</BreadcrumbPage>
											</BreadcrumbItem>
										</>
									) : (
										<BreadcrumbItem>
											<BreadcrumbPage>{application?.name}</BreadcrumbPage>
										</BreadcrumbItem>
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
		<SidebarMenuItem className="mb-2">
			<SidebarMenuButton
				isActive={isActive}
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
