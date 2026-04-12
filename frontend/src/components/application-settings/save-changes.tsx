import { Save } from "lucide-react";
import { Button } from "../ui/button";

export function SaveChanges() {
    return (
        <div>
            <Button>
                <Save className="mr-2 h-4 w-4" />
                Save Changes
            </Button>
        </div>
    )
}
