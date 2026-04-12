import { Trash2 } from "lucide-react";
import { Button } from "../ui/button";

export function Danger({ activeSection }: { activeSection: string }) {
    return (
        <div>
            {activeSection === "danger" && (
                <div className="space-y-6">
                    <div className="bg-card border border-destructive/50 rounded-lg p-6 space-y-6">
                        <h2 className="text-lg font-semibold text-destructive">Danger Zone</h2>

                        <div className="space-y-4">
                            <div className="flex items-center justify-between p-4 bg-muted rounded-lg">
                                <div>
                                    <p className="font-medium">Delete Application</p>
                                    <p className="text-sm text-muted-foreground">
                                        Permanently delete this application and all its data
                                    </p>
                                </div>
                                <Button variant="destructive">
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    Delete
                                </Button>
                            </div>
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}
