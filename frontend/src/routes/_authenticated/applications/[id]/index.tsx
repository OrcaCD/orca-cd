import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/applications/id/')({
  component: RouteComponent,
})

function RouteComponent() {
  return <div>Hello "/_authenticated/applications/id/"!</div>
}
