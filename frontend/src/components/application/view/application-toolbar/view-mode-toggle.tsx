import { LayoutGrid, List } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

type Props = {
  viewMode: "grid" | "list"
  setViewMode: (value: "grid" | "list") => void
}

export function ViewModeToggle({ viewMode, setViewMode }: Props) {
  return (
    <div className="flex border border-border rounded-md">
      <Button
        variant="ghost"
        size="icon"
        className={cn(viewMode === "grid" && "bg-muted")}
        onClick={() => setViewMode("grid")}
      >
        <LayoutGrid className="h-4 w-4" />
      </Button>

      <Button
        variant="ghost"
        size="icon"
        className={cn(viewMode === "list" && "bg-muted")}
        onClick={() => setViewMode("list")}
      >
        <List className="h-4 w-4" />
      </Button>
    </div>
  )
}
