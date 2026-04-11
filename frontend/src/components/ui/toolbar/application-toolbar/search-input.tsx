import { Search } from "lucide-react"
import { Input } from "@/components/ui/input"

type Props = {
  searchQuery: string
  setSearchQuery: (value: string) => void
}

export function SearchInput({ searchQuery, setSearchQuery }: Props) {
  return (
    <div className="relative flex-1">
      <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
      <Input
        placeholder="Search applications..."
        className="pl-9 bg-muted border-border"
        value={searchQuery}
        onChange={(e) => setSearchQuery(e.target.value)}
      />
    </div>
  )
}
