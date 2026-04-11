import type { Application } from "@/lib/applications";
import { Link } from "lucide-react";

export function ApplicationCard({ app }: { app: Application }) {
    return (
        <Link
            key={app.id}
            href={`/applications/${app.id}`}
            className="group bg-card border border-border rounded-lg p-4 hover:border-primary/50 transition-colors"
        >
        </Link>
    )
}
