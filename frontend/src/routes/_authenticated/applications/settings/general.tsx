import { ApplicationForm } from "@/components/application-form";
import { m } from "@/lib/paraglide/messages";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/applications/settings/general")({
	component: CreateApplicationPage,
	head: () => ({
		meta: [
			{
				title: `${m.navApplications()} - ${m.settings()}`,
			},
		],
	}),
});

function CreateApplicationPage() {
	return <ApplicationForm application={null} />;
}
