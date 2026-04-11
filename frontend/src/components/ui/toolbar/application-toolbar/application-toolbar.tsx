import {
    SearchInput,
    ProjectFilter,
    ViewModeToggle,
} from "@/components/ui/toolbar/application-toolbar"
import { RefreshCw } from "lucide-react"
import { Button } from "../../button"

type Props = {
    viewMode: "grid" | "list"
    setViewMode: (value: "grid" | "list") => void
    searchQuery: string
    setSearchQuery: (value: string) => void
    projectFilter: string
    setProjectFilter: (value: string) => void
}

export function ApplicationFilters({ viewMode, setViewMode, searchQuery, setSearchQuery, projectFilter, setProjectFilter }: Props) {
    return (
        <div className="flex flex-col sm:flex-row gap-4">
            <SearchInput searchQuery={searchQuery} setSearchQuery={setSearchQuery} />

            <div className="flex gap-2">
                <ProjectFilter projectFilter={projectFilter} setProjectFilter={setProjectFilter} />

                <Button variant="outline" size="icon">
                    <RefreshCw className="h-4 w-4" />
                </Button>

                <ViewModeToggle viewMode={viewMode} setViewMode={setViewMode} />
            </div>
        </div>
    )
}
