import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/applications/settings/")({
	beforeLoad() {
		throw redirect({
			to: "/applications/settings/general",
		});
	},
});
