import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Save } from "lucide-react";
import { Button } from "@/components/ui/button";

export function General({ activeSection }: { activeSection: string }) {
	return (
		<div>
			{activeSection === "general" && (
				<div className="space-y-6">
					<div className="bg-card border border-border rounded-lg p-6 space-y-6">
						<h2 className="text-lg font-semibold">General Settings</h2>

						<div className="space-y-4">
							<div className="space-y-2">
								<Label htmlFor="name">Application Name</Label>
								<Input id="name" defaultValue="api-gateway" className="bg-muted border-border" />
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
