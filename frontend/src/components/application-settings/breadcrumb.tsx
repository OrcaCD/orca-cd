import { Link } from "@tanstack/react-router";

export function Breadcrumb({ id }: { id: string }) {
    return (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Link to="/applications" className="hover:text-foreground">
            Applications
          </Link>
          <span>/</span>
          <Link to="/applications/$id" params={{id: id}} className="hover:text-foreground">
            api-gateway
          </Link>
          <span>/</span>
          <span className="text-foreground">Settings</span>
        </div>
    )
}
