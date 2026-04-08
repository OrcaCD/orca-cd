import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/agents/")({
	component: RouteComponent,
	head: () => ({
		meta: [
			{
				title: "Agents",
			},
		],
	}),
});

function RouteComponent() {
	return <div>Hello "/_authenticated/agents/"!</div>;
}
