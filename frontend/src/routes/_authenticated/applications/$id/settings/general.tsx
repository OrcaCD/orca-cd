import { type Application } from "@/lib/applications";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import { createFileRoute } from "@tanstack/react-router";
import { ApplicationForm } from "@/components/application-form";

export const Route = createFileRoute("/_authenticated/applications/$id/settings/general")({
	component: EditApplicationPage,
	head: () => ({
		meta: [
			{
				title: `${m.navApplications()} - ${m.settings()}`,
			},
		],
	}),
});

function EditApplicationPage() {
	const { id } = Route.useParams();
	const { data: application } = useFetch<Application>(`/applications/${id}`);

	return (
		<div className="p-6 space-y-6">
			{application ? <ApplicationForm application={application} /> : <div>{m.loadingDots()}</div>}
		</div>
	);
}
