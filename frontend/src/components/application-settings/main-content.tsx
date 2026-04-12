import { General } from "./general";
import { Source } from "./source";
import { Sync } from "./sync";
import { Notifications } from "./notifications";
import { Danger } from "./danger";

export function MainContent({ activeSection }: { activeSection: string }) {
    return (
        <div className="flex-1 space-y-6">
            <General activeSection={activeSection} />
            <Source activeSection={activeSection} />
            <Sync activeSection={activeSection} />
            <Notifications activeSection={activeSection} />
            <Danger activeSection={activeSection} />
        </div>
    )
}
