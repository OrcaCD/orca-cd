import {
    Sidebar, SidebarContent, SidebarGroup, SidebarGroupContent,
    SidebarGroupLabel, SidebarHeader, SidebarInset, SidebarMenu,
    SidebarMenuButton, SidebarMenuItem, SidebarProvider, SidebarTrigger, useSidebar,
} from "@/components/ui/sidebar";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Settings } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import type React from "react";

const settingsPages = [
    {
        title: () => m.general(),
        icon: Settings,
        pathWithId: "/applications/$id/settings/general",
        pathWithoutId: "/applications/settings/general",
    },
];

export function SharedSettingsLayout({
    id,
    breadcrumbs
}: {
    id?: string;
    breadcrumbs?: React.ReactNode;
}) {
    const { location } = useRouterState();

    return (
        <div className="flex flex-col min-h-[calc(100svh-3.5rem)] w-full">
            {breadcrumbs && (
                <div className="px-6 pt-6 pb-2">
                    {breadcrumbs}
                </div>
            )}
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
                            <SidebarGroupLabel>{m.management()}</SidebarGroupLabel>
                            <SidebarGroupContent>
                                <SidebarMenu>
                                    {settingsPages.map((item) => (
                                        <SettingsSidebarMenuItem
                                            key={item.pathWithId}
                                            item={item}
                                            pathname={location.pathname}
                                            id={id}
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
        </div>
    );
}

function SettingsSidebarMenuItem({
    item,
    pathname,
    id,
}: {
    item: { title: () => string; icon: any; pathWithId: string; pathWithoutId: string };
    pathname: string;
    id?: string;
}) {
    const { isMobile, setOpenMobile } = useSidebar();

    const isActive = pathname.includes("/settings/general");

    return (
        <SidebarMenuItem className="mb-2">
            <SidebarMenuButton asChild isActive={isActive}>
                {id ? (
                    <Link
                        to={item.pathWithId}
                        params={{ id }}
                        onClick={() => isMobile && setOpenMobile(false)}
                    >
                        <item.icon />
                        <span>{item.title()}</span>
                    </Link>
                ) : (
                    <Link
                        to={item.pathWithoutId}
                        onClick={() => isMobile && setOpenMobile(false)}
                    >
                        <item.icon />
                        <span>{item.title()}</span>
                    </Link>
                )}
            </SidebarMenuButton>
        </SidebarMenuItem>
    );
}
