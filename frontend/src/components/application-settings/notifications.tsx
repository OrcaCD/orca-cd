import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { SaveChanges } from "./save-changes";

export function Notifications({ activeSection }: { activeSection: string }) {
    return (
        <div>
            {activeSection === "notifications" && (
                <div className="space-y-6">
                    <div className="bg-card border border-border rounded-lg p-6 space-y-6">
                        <h2 className="text-lg font-semibold">Notification Settings</h2>

                        <div className="space-y-4">
                            <div className="space-y-2">
                                <Label htmlFor="webhook">Webhook URL</Label>
                                <Input
                                    id="webhook"
                                    placeholder="https://hooks.slack.com/..."
                                    className="bg-muted border-border"
                                />
                            </div>

                            <div className="space-y-3">
                                <Label>Notify On</Label>
                                <div className="space-y-2">
                                    {["Sync Success", "Sync Failed", "Health Degraded", "Out of Sync"].map((event) => (
                                        <div key={event} className="flex items-center space-x-2">
                                            <Switch
                                                id={event.toLowerCase().replace(" ", "-")}
                                                defaultChecked={event !== "Sync Success"}
                                            />
                                            <Label htmlFor={event.toLowerCase().replace(" ", "-")} className="text-sm">
                                                {event}
                                            </Label>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        </div>

                        <SaveChanges />
                    </div>
                </div>
            )}
        </div>
    )
}
