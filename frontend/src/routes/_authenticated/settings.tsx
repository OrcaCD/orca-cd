import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/settings")({
	beforeLoad: ({ context }) => {
		if (!context.auth.isAdmin) {
			throw redirect({ to: "/" });
		}
	},
	component: SettingsLayout,
});

function SettingsLayout() {
	return (
		<div className="container mx-auto max-w-4xl py-8 px-4">
			<Outlet />
		</div>
	);
}
