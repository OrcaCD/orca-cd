import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { m } from "@/lib/paraglide/messages";
import type { AuditLog } from '@/lib/audit-log';
import { AuditLogsDataTable } from '@/components/tables/audit-log/data-table';
import { columns } from '@/components/tables/audit-log/columns';

export const Route = createFileRoute('/_authenticated/admin/audit-log')({
  loader: async (): Promise<AuditLog[]> => {
    const response = await fetch('/api/v1/admin/audit-logs')
    return response.json()
  },
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
  const data = useLoaderData({ from: '/_authenticated/admin/audit-log' });
  return (
    <div className='flex flex-col gap-6'>
      <div>
        <h1 className='text-2xl font-bold'>{m.adminAuditLog()}</h1>
        <p className='text-muted-foreground text-sm'>{m.adminAuditLogDescription()}</p>
      </div>

      <AuditLogsDataTable columns={columns} data={data} />
    </div>
  )
}
