import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Input } from "@/components/ui/input";
import { SaveChanges } from "./save-changes";

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

                            <div className="space-y-2">
                                <Label htmlFor="project">Project</Label>
                                <Select defaultValue="production">
                                    <SelectTrigger className="bg-muted border-border">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="production">Production</SelectItem>
                                        <SelectItem value="staging">Staging</SelectItem>
                                        <SelectItem value="development">Development</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>

                            <div className="space-y-2">
                                <Label htmlFor="description">Description</Label>
                                <Textarea
                                    id="description"
                                    placeholder="Describe this application..."
                                    className="bg-muted border-border"
                                    defaultValue="Main API gateway handling all incoming requests"
                                />
                            </div>
                        </div>

                        <SaveChanges />
                    </div>
                </div>
            )}
        </div>
    )
}
