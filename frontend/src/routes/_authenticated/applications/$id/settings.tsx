import { createFileRoute } from "@tanstack/react-router";
import { ApplicationsLayout } from "./index";

export const Route = createFileRoute("/_authenticated/applications/$id/settings")({
	component: SettingsLayout,
});

function SettingsLayout() {
	const { id } = Route.useParams();

	return <ApplicationsLayout id={id} />;
}
