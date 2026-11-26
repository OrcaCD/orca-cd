import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { takeLoginRedirect } from "@/lib/oauth";

export const Route = createFileRoute("/_authenticated")({
	beforeLoad: ({ context }) => {
		if (!context.auth.isAuthenticated) {
			throw redirect({
				to: "/login",
			});
		}
		// Back to the consent page the user had to log in for. Reloading the
		// page keeps its query as the client sent it.
		const loginRedirect = takeLoginRedirect();
		if (loginRedirect) {
			throw redirect({ href: loginRedirect, reloadDocument: true });
		}
	},
	component: () => <Outlet />,
});
