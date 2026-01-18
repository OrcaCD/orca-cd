import { TanStackDevtools } from "@tanstack/react-devtools";
import { createRootRoute, HeadContent, Outlet } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";

import { ThemeProvider } from "@/components/theme-provider";
import Navbar from "../components/navbar";

export const Route = createRootRoute({
	component: () => (
		<>
			<HeadContent />
			<ThemeProvider defaultTheme="dark" storageKey="orca-theme">
				<div className="min-h-screen bg-background">
					<Navbar />
					<Outlet />
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
	),
	head: () => ({
		meta: [
			{
				name: "description",
				content: "My App is a web application",
			},
			{
				title: "OrcaCD",
			},
		],
		links: [
			{
				rel: "icon",
				href: "/assets/favicon.ico",
			},
		],
	}),
});
