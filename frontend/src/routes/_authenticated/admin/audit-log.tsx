import { createFileRoute } from '@tanstack/react-router'
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute('/_authenticated/admin/audit-log')({
  component: AuditLogPage,
  head: () => ({
      meta: [
        {
          title: `${m.admin()} - ${m.adminAuditLog()}`,
        },
      ],
    }),
})

function AuditLogPage() {
  return <div>Hello "/_authenticated/admin/audit-log"!</div>
}
