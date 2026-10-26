import { createRouter, RouterProvider } from "@tanstack/react-router";
import { CompassIcon, TriangleAlertIcon } from "lucide-react";
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";

// Import the generated route tree
import { routeTree } from "./routeTree.gen";

import "./styles.css";
import { ErrorPage } from "./components/error-page";
import { AuthProvider, useAuth } from "./lib/auth";
import type { RouterContext } from "./routes/__root";
import { m } from "@/lib/paraglide/messages";
import { initializeI18n } from "@/lib/i18n";

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
			<ErrorPage
				icon={<CompassIcon />}
				code="404"
				title={m.pageNotFound()}
				description={m.pageNotFoundDescription()}
			/>
		);
	},
	defaultErrorComponent: ({ error, reset }) => {
		return (
			<ErrorPage
				icon={<TriangleAlertIcon />}
				title={m.somethingWentWrong()}
				description={m.somethingWentWrongDescription()}
				details={error instanceof Error ? error.message : String(error)}
				onRetry={reset}
			/>
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

function renderApp() {
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
}

async function bootstrap() {
	try {
		await initializeI18n();
	} finally {
		renderApp();
	}
}

void bootstrap();
