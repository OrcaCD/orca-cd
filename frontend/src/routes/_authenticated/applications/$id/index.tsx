import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/applications/$id/")({
	beforeLoad({ params }) {
		throw redirect({
			to: "/applications/$id/details",
			params: {
				id: params.id,
			},
		});
	},
});
