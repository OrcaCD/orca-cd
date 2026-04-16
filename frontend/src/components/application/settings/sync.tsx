import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Save } from "lucide-react";

export function Sync({ activeSection }: { activeSection: string }) {
	const [autoSync, setAutoSync] = useState(true);
	const [selfHeal, setSelfHeal] = useState(true);
	const [pruneResources, setPruneResources] = useState(false);
	return (
		<div>
			{activeSection === "sync" && (
				<div className="space-y-6">
					<div className="bg-card border border-border rounded-lg p-6 space-y-6">
						<h2 className="text-lg font-semibold">Sync Policy</h2>

						<div className="space-y-6">
							<div className="flex items-center justify-between">
								<div className="space-y-1">
									<Label>Auto-Sync</Label>
									<p className="text-sm text-muted-foreground">
										Automatically sync when changes are detected
									</p>
								</div>
								<Switch checked={autoSync} onCheckedChange={setAutoSync} />
							</div>

							<div className="flex items-center justify-between">
								<div className="space-y-1">
									<Label>Self-Heal</Label>
									<p className="text-sm text-muted-foreground">
										Automatically correct drift from desired state
									</p>
								</div>
								<Switch checked={selfHeal} onCheckedChange={setSelfHeal} />
							</div>

							<div className="flex items-center justify-between">
								<div className="space-y-1">
									<Label>Prune Resources</Label>
									<p className="text-sm text-muted-foreground">
										Remove containers not defined in manifest
									</p>
								</div>
								<Switch checked={pruneResources} onCheckedChange={setPruneResources} />
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
