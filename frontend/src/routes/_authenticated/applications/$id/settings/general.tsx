import { type Application } from "@/lib/applications";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import { createFileRoute, Link } from "@tanstack/react-router";
import { ApplicationForm } from "@/components/application-from";
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";

export const Route = createFileRoute(
  '/_authenticated/applications/$id/settings/general',
)({
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
    <div className="space-y-6">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link to="/applications">{m.pageApplications()}</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link to="/applications/$id" params={{ id }}>
                {application?.name}
              </Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>{m.settings()}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>

      <ApplicationForm application={application} />
    </div>
  );
}
