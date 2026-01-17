import { TanStackDevtools } from "@tanstack/react-devtools";
import { createRootRoute, Outlet } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";

import { ThemeProvider } from "@/components/theme-provider";
import Navbar from "../components/navbar";

export const Route = createRootRoute({
	component: () => (
		<ThemeProvider defaultTheme="dark" storageKey="orca-theme">
			<div
			className="min-h-screen bg-background">

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
	),
});
