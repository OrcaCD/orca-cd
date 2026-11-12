import type { ReactNode } from "react";
import { Breadcrumb, BreadcrumbList } from "@/components/ui/breadcrumb";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { Separator } from "@/components/ui/separator";
import {
	Sidebar,
	SidebarContent,
	SidebarInset,
	SidebarProvider,
	SidebarTrigger,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { m } from "@/lib/paraglide/messages";
import { isApple } from "@/lib/utils";

/** Section navigation uses the same quiet line marker as the primary navigation. */
export const sidebarMenuButtonClass =
	"relative text-muted-foreground hover:bg-transparent hover:text-foreground active:bg-transparent data-active:bg-transparent data-active:text-foreground before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:rounded-full before:bg-primary before:opacity-0 data-active:before:opacity-100";

interface SidebarLayoutProps {
	/** Sidebar groups. */
	sidebar: ReactNode;
	/** Breadcrumb items rendered next to the sidebar toggle. */
	breadcrumb: ReactNode;
	children: ReactNode;
}

/**
 * Shell of the pages with a section sidebar (application, repository, settings and admin). The
 * sidebar is fixed below the sticky app header, whose height is provided by AppHeader.
 */
export function SidebarLayout({ sidebar, breadcrumb, children }: SidebarLayoutProps) {
	return (
		<SidebarProvider className="min-h-[calc(100svh-var(--app-header-height))]">
			<Sidebar collapsible="icon" className="border-r md:top-(--app-header-height) md:h-auto">
				<SidebarContent className="pt-2">{sidebar}</SidebarContent>
			</Sidebar>
			<SidebarInset className="min-w-0">
				<div className="mx-auto w-full max-w-screen-2xl space-y-6 overflow-y-auto px-4 py-5 sm:px-6 lg:px-8 lg:py-6">
					<div className="flex h-8 items-center gap-2">
						<Tooltip>
							<TooltipTrigger
								render={<SidebarTrigger className="-ml-1.5 text-muted-foreground" />}
							/>
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
							className="mr-1.5 my-auto data-[orientation=vertical]:h-4"
						/>
						<Breadcrumb className="min-w-0">
							<BreadcrumbList>{breadcrumb}</BreadcrumbList>
						</Breadcrumb>
					</div>
					{children}
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
