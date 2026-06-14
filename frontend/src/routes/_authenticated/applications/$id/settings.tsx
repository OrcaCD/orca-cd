import { createFileRoute } from "@tanstack/react-router";
import { SharedSettingsLayout } from "@/components/applications-settings";

export const Route = createFileRoute("/_authenticated/applications/$id/settings")({
	component: SettingsLayoutWithId,
});

function SettingsLayoutWithId() {
	const { id } = Route.useParams();

	return <SharedSettingsLayout id={id} />;
}
