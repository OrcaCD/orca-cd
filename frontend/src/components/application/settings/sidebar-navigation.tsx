import { cn } from "@/lib/utils";
import { GitBranch, RefreshCw, Server, Shield } from "lucide-react";

export function SidebarNavigation({
	activeSection,
	setActiveSection,
}: {
	activeSection: string;
	setActiveSection: (section: string) => void;
}) {
	const settingsSections = [
		{ id: "general", label: "General", icon: Server },
		{ id: "source", label: "Source", icon: GitBranch },
		{ id: "sync", label: "Sync Policy", icon: RefreshCw },
		{ id: "danger", label: "Danger Zone", icon: Shield },
	];

	return (
		<aside className="lg:w-56 shrink-0">
			<nav className="space-y-1">
				{settingsSections.map((section) => (
					<button
						key={section.id}
						onClick={() => setActiveSection(section.id)}
						className={cn(
							"w-full flex items-center gap-2 px-3 py-2 text-sm rounded-md transition-colors text-left",
							activeSection === section.id
								? "bg-primary/10 text-primary"
								: "text-muted-foreground hover:text-foreground hover:bg-muted",
						)}
					>
						<section.icon className="h-4 w-4" />
						{section.label}
					</button>
				))}
			</nav>
		</aside>
	);
}
