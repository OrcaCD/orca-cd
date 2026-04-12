import { Breadcrumb } from '@/components/application-settings/breadcrumb'
import { MainContent } from '@/components/application-settings/main-content'
import { SidebarNavigation } from '@/components/application-settings/sidebar-navigation'
import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

export const Route = createFileRoute(
  '/_authenticated/applications/$id/settings',
)({
  component: SettingsPage,
  head: () => ({
    meta: [
      {
        title: "Settings",
      },
    ],
  }),
})

function SettingsPage() {
  const { id } = Route.useParams()
  const [activeSection, setActiveSection] = useState("general")
  return (
    <div className="p-6 space-y-6">
      <Breadcrumb id={id} />
      <div className="flex flex-col lg:flex-row gap-6">
        <SidebarNavigation activeSection={activeSection} setActiveSection={setActiveSection} />
        <MainContent activeSection={activeSection} />
      </div>
    </div>
  )
}
