import { createRouter, Link, RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";

// Import the generated route tree
import { routeTree } from "./routeTree.gen";

import "./styles.css";
import { Button } from "./components/ui/button";
import { AuthProvider, useAuth } from "./lib/auth";
import type { RouterContext } from "./routes/__root";
import { m } from "@/lib/paraglide/messages";

// Create a new router instance
const router = createRouter({
	routeTree,
	context: {
		auth: undefined!,
	} as RouterContext,
	defaultPreload: "intent",
	scrollRestoration: true,
	defaultStructuralSharing: true,
	defaultPreloadStaleTime: 0,
	defaultNotFoundComponent: () => {
		return (
			<div className="text-center">
				<div className="min-h-screen flex flex-col items-center justify-center bg-background text-foreground text-[calc(10px+2vmin)]">
					<p>{m.pageNotFound()}</p>
					<Link to="/">
						<Button size="lg">{m.goHome()}</Button>
					</Link>
				</div>
			</div>
		);
	},
	defaultErrorComponent: ({ error }) => {
		return (
			<div className="text-center">
				<div className="min-h-screen flex flex-col items-center justify-center bg-background text-foreground text-[calc(10px+2vmin)]">
					<p>{m.somethingWentWrong()}</p>
					<pre>{error.message}</pre>
					<Link to="/">
						<Button size="lg">{m.goHome()}</Button>
					</Link>
				</div>
			</div>
		);
	},
});

// Register the router instance for type safety
declare module "@tanstack/react-router" {
	interface Register {
		router: typeof router;
	}
}

function InnerApp() {
	const { auth } = useAuth();
	if (auth.isLoading) {
		return null;
	}
	return <RouterProvider router={router} context={{ auth }} />;
}

// Render the app
const rootElement = document.getElementById("app");
if (rootElement && !rootElement.innerHTML) {
	const root = ReactDOM.createRoot(rootElement);
	root.render(
		<StrictMode>
			<AuthProvider>
				<InnerApp />
			</AuthProvider>
		</StrictMode>,
	);
}
