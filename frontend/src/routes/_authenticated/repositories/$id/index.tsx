import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/repositories/$id/")({
	beforeLoad({ params }) {
		throw redirect({
			to: "/repositories/$id/settings/auth",
			params: { id: params.id },
		});
	},
});
