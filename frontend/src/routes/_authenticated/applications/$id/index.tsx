import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/applications/$id/")({
	beforeLoad({ params }) {
		throw redirect({
			to: "/applications/$id/details",
			params: {
				id: params.id,
			},
		});
	},
});

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
import { Link, Outlet } from "@tanstack/react-router";
import { Settings } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import type { Application } from "@/lib/applications";
import { useFetch } from "@/lib/api";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";

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
			icon: any;
			to: string;
		}[];
	};

const sidebarItems: SidebarItem[] = [
	{
		type: "link",
		title: () => "Details",
		icon: Settings,
		to: "/applications/$id/details"
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
		],
	},
];

export function ApplicationsLayout({
	id
}: {
	id?: string;
}) {
	const { data: application } = useFetch<Application>(`/applications/${id}`);

	return (
		<div className="flex flex-col min-h-[calc(100svh-3.5rem)] w-full">
			<SidebarProvider className="min-h-[calc(100svh-3.5rem)]">
				<Sidebar className="border-r md:top-14">
					<SidebarHeader className="px-4 py-5">
						<div className="flex items-center gap-2 font-semibold">
							<StaticLucideIcon name={application?.icon} className="h-7 w-7 text-primary" />
							{application?.name}
						</div>
					</SidebarHeader>
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
											<SidebarGroup key={item.title()}>
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
		icon: any;
		to: string;
	};
	pathname: string;
	id?: string;
}) {
	const { isMobile, setOpenMobile } = useSidebar();

	const targetPath = item.to.replace("$id", id ?? "");
	const isActive = pathname === targetPath || pathname.startsWith(`${targetPath}/`);

	return (
		<SidebarMenuItem className="mb-2">
			<SidebarMenuButton isActive={isActive}
				render={
					<Link
						to={item.to}
						params={{ id: id ?? "" }}
						onClick={() => isMobile && setOpenMobile(false)}
					>
						<item.icon className="h-4 w-4" />
						<span>{item.title()}</span>
					</Link>
				}
			></SidebarMenuButton>
		</SidebarMenuItem>
	);
}
