import { SharedSettingsLayout } from "@/components/applications-settings";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/applications/settings")({
	component: SettingsLayoutWithoutId,
});

function SettingsLayoutWithoutId() {
	return <SharedSettingsLayout />;
}
