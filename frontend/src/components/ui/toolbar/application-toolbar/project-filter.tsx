import { Filter } from "lucide-react"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

type Props = {
  projectFilter: string
  setProjectFilter: (value: string) => void
}

export function ProjectFilter({ projectFilter, setProjectFilter }: Props) {
  return (
    <Select value={projectFilter} onValueChange={setProjectFilter}>
      <SelectTrigger className="w-44 bg-muted border-border flex items-center gap-2 px-3">
        <Filter className="h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="flex-1 truncate">
          <SelectValue placeholder="Project" />
        </div>
      </SelectTrigger>

      <SelectContent>
        <SelectItem value="all">All Projects</SelectItem>
        <SelectItem value="production">Production</SelectItem>
        <SelectItem value="staging">Staging</SelectItem>
      </SelectContent>
    </Select>
  )
}
