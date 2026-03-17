import { TanStackDevtools } from "@tanstack/react-devtools";
import { createRootRoute, HeadContent, Outlet, useRouterState } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";

import { ThemeProvider } from "@/components/theme-provider";
import Navbar from "../components/navbar";
import { Toaster } from "@/components/ui/sonner";

function RootComponent() {
	const { location } = useRouterState();
	const showNavbar = location.pathname !== "/login";

	return (
		<>
			<HeadContent />
			<ThemeProvider defaultTheme="dark" storageKey="orca-theme">
				<div className="min-h-screen bg-background">
					{showNavbar && <Navbar />}
					<Outlet />
					<Toaster />
					<TanStackDevtools
						config={{
							position: "bottom-right",
						}}
						plugins={[
							{
								name: "Tanstack Router",
								render: <TanStackRouterDevtoolsPanel />,
							},
						]}
					/>
				</div>
			</ThemeProvider>
		</>
	);
}

export const Route = createRootRoute({
	component: RootComponent,
	head: () => ({
		meta: [
			{
				name: "description",
				content: "GitOps for Docker",
			},
			{
				title: "OrcaCD",
				charset: "utf-8",
				viewport: "width=device-width, initial-scale=1",
				themeColor: "#0f172a",
			},
		],
		links: [
			{
				rel: "icon",
				href: "/assets/favicon.ico",
			},
			{
				rel: "apple-touch-icon",
				href: "/assets/apple-touch-icon.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "32x32",
				href: "/assets/logo-dark-32.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "64x64",
				href: "/assets/logo-dark-64.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "96x96",
				href: "/assets/logo-dark-96.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "128x128",
				href: "/assets/logo-dark-128.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "144x144",
				href: "/assets/logo-dark-144.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "180x180",
				href: "/assets/logo-dark-180.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "192x192",
				href: "/assets/logo-dark-192.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "256x256",
				href: "/assets/logo-dark-256.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "512x512",
				href: "/assets/logo-dark-512.png",
			},
			{
				rel: "icon",
				type: "image/png",
				sizes: "1024x1024",
				href: "/assets/logo-dark-1024.png",
			},
		],
	}),
});
