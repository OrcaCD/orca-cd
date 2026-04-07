import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/admin")({
	beforeLoad: ({ context }) => {
		if (!context.auth.isAdmin) {
			throw redirect({ to: "/" });
		}
	},
	component: AdminLayout,
});

function AdminLayout() {
	return (
		<div className="p-6 space-y-6">
			<Outlet />
		</div>
	);
}
