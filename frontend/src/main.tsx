import { createRouter, Link, RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";

// Import the generated route tree
import { routeTree } from "./routeTree.gen";

import "./styles.css";
import { Button } from "./components/ui/button";

// Create a new router instance
const router = createRouter({
	routeTree,
	context: {},
	defaultPreload: "intent",
	scrollRestoration: true,
	defaultStructuralSharing: true,
	defaultPreloadStaleTime: 0,
	defaultNotFoundComponent: () => {
		return (
			<div className="text-center">
				<div className="min-h-screen flex flex-col items-center justify-center bg-background text-foreground text-[calc(10px+2vmin)]">
					<p>Not Found</p>
					<Link to="/">
						<Button>Go Home</Button>
					</Link>
				</div>
			</div>
		);
	},
	defaultErrorComponent: ({ error }) => {
		return (
			<div className="text-center">
				<div className="min-h-screen flex flex-col items-center justify-center bg-background text-foreground text-[calc(10px+2vmin)]">
					<p>Something went wrong</p>
					<pre>{error.message}</pre>
					<Link to="/">
						<Button>Go Home</Button>
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

// Render the app
const rootElement = document.getElementById("app");
if (rootElement && !rootElement.innerHTML) {
	const root = ReactDOM.createRoot(rootElement);
	root.render(
		<StrictMode>
			<RouterProvider router={router} />
		</StrictMode>,
	);
}
