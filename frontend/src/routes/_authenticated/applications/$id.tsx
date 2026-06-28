import { Breadcrumb, BreadcrumbList, BreadcrumbItem, BreadcrumbLink, BreadcrumbSeparator, BreadcrumbPage } from "@/components/ui/breadcrumb";
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
import { useFetch } from "@/lib/api";
import type { Application } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Info, Settings, RefreshCw } from "lucide-react";

export const Route = createFileRoute("/_authenticated/applications/$id")({
	component: ApplicationsLayout,
});

type SidebarItem =
	| {
		type: "link";
		title: () => string;
		icon: any;
		to: string;
	}
	| {
		type: "group";
		title: () => string;
		children: {
			title: () => string;
			icon?: any;
			to: string;
		}[];
	};

const sidebarItems: SidebarItem[] = [
	{
		type: "link",
		title: () => m.details(),
		icon: Info,
		to: "/applications/$id/details",
	},
	{
		type: "group",
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

	return (
		<div className="flex flex-col min-h-[calc(100svh-3.5rem)] w-full">
			<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
				<Sidebar className="border-r md:top-14">
					<SidebarContent>
						<SidebarGroup>
							<SidebarGroupLabel>{m.management()}</SidebarGroupLabel>
							<SidebarGroupContent>
								<SidebarMenu>
									{sidebarItems.map((item) => {
										if (item.type === "link") {
											return (
												<SidebarItem
													key={item.to}
													item={item}
													pathname={location.pathname}
													id={id}
												/>
											);
										}

										return (
											<SidebarGroup key={item.title()} className="p-0">
												<SidebarGroupLabel>{item.title()}</SidebarGroupLabel>

												<SidebarGroupContent>
													{item.children.map((child) => (
														<SidebarItem
															key={child.to}
															item={child}
															pathname={location.pathname}
															id={id}
														/>
													))}
												</SidebarGroupContent>
											</SidebarGroup>
										);
									})}
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
						<Breadcrumb>
							<BreadcrumbList>
								<BreadcrumbItem>
									<BreadcrumbLink
										render={<Link to="/applications">{m.pageApplications()}</Link>}
									></BreadcrumbLink>
								</BreadcrumbItem>
								<BreadcrumbSeparator />
								<BreadcrumbItem>
									<BreadcrumbPage>{application?.name}</BreadcrumbPage>
								</BreadcrumbItem>
							</BreadcrumbList>
						</Breadcrumb>
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
