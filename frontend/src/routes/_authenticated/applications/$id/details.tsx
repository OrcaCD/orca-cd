import { createFileRoute } from "@tanstack/react-router";
import { ApplicationsLayout } from "./index";

export const Route = createFileRoute("/_authenticated/applications/$id/details")({
	component: DetailsLayout,
});

function DetailsLayout() {
	const { id } = Route.useParams();

	return <ApplicationsLayout id={id} />;
}
