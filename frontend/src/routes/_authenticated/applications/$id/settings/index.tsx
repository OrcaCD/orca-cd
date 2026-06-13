import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute("/_authenticated/applications/$id/settings/")({
  beforeLoad({ params }) {
    throw redirect({
      to: "/applications/$id/settings/general",
      params: {
        id: params.id,
      },
    });
  }
});
