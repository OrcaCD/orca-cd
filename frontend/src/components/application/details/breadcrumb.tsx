import type { Application } from "@/lib/applications"
import { Link } from "@tanstack/react-router"
import { ArrowLeft } from "lucide-react"

export function Breadcrumb({ app }: { app: Application }) {
    return (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Link to="/applications" className="hover:text-foreground flex items-center gap-1">
                <ArrowLeft className="h-4 w-4" />
                Applications
            </Link>
            <span>/</span>
            <span className="text-foreground">{app.name}</span>
        </div>
    )
}
