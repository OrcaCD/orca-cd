import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/applications/")({
	component: RouteComponent,
	head: () => ({
		meta: [
			{
				title: "Applications",
			},
		],
	}),
});

function RouteComponent() {
	return <div>Hello "/_authenticated/applications/"!</div>;
}
