import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Save } from "lucide-react";

export function Source({ activeSection }: { activeSection: string }) {
	return (
		<div>
			{activeSection === "source" && (
				<div className="space-y-6">
					<div className="bg-card border border-border rounded-lg p-6 space-y-6">
						<h2 className="text-lg font-semibold">Source Repository</h2>

						<div className="space-y-4">
							<div className="space-y-2">
								<Label htmlFor="repo">Repository URL</Label>
								<Input
									id="repo"
									defaultValue="https://github.com/org/api-gateway"
									className="bg-muted border-border"
								/>
							</div>

							<div className="space-y-2">
								<Label htmlFor="branch">Target Branch</Label>
								<Input id="branch" defaultValue="main" className="bg-muted border-border" />
							</div>

							<div className="space-y-2">
								<Label htmlFor="path">Compose File Path</Label>
								<Input
									id="path"
									defaultValue="/docker-compose.yml"
									className="bg-muted border-border"
								/>
							</div>
						</div>

						<Button>
							<Save className="mr-2 h-4 w-4" />
							Save Changes
						</Button>
					</div>

					<div className="bg-card border border-border rounded-lg p-6 space-y-6">
						<h2 className="text-lg font-semibold">Target Host</h2>

						<div className="space-y-4">
							<div className="space-y-2">
								<Label htmlFor="host">Host</Label>
								<Select defaultValue="prod-server-01">
									<SelectTrigger className="bg-muted border-border">
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="prod-server-01">prod-server-01</SelectItem>
										<SelectItem value="prod-server-02">prod-server-02</SelectItem>
										<SelectItem value="staging-server">staging-server</SelectItem>
									</SelectContent>
								</Select>
							</div>

							<div className="space-y-2">
								<Label htmlFor="deployPath">Deployment Path</Label>
								<Input
									id="deployPath"
									defaultValue="/opt/apps/api-gateway"
									className="bg-muted border-border"
								/>
							</div>
						</div>

						<Button>
							<Save className="mr-2 h-4 w-4" />
							Save Changes
						</Button>
					</div>
				</div>
			)}
		</div>
	);
}
