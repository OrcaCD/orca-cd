import { Filter } from "lucide-react"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

type Props = {
  projectFilter: string
  setProjectFilter: (value: string) => void
}

export function ProjectFilter({ projectFilter, setProjectFilter }: Props) {
  return (
    <Select value={projectFilter} onValueChange={setProjectFilter}>
      <SelectTrigger className="w-40 bg-muted border-border">
        <Filter className="mr-2 h-4 w-4" />
        <SelectValue placeholder="Project" />
      </SelectTrigger>

      <SelectContent>
        <SelectItem value="all">All Projects</SelectItem>
        <SelectItem value="production">Production</SelectItem>
        <SelectItem value="staging">Staging</SelectItem>
      </SelectContent>
    </Select>
  )
}
