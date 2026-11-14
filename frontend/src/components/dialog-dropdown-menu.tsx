import { createContext, useContext, useState, type ComponentProps } from "react";
import { DropdownMenu, DropdownMenuContent } from "@/components/ui/dropdown-menu";

const KeepMountedContext = createContext(false);

/**
 * Dropdown menu whose items open dialogs. The dialogs live inside the menu, so it has to stay
 * mounted after it closes, or they would close with it. Until the menu is opened for the first
 * time nothing of it is rendered, which keeps lists with a menu per item cheap.
 */
export function DialogDropdownMenu({
	onOpenChange,
	...props
}: ComponentProps<typeof DropdownMenu>) {
	const [opened, setOpened] = useState(false);

	return (
		<KeepMountedContext value={opened}>
			<DropdownMenu
				{...props}
				onOpenChange={(open, eventDetails) => {
					if (open) {
						setOpened(true);
					}
					onOpenChange?.(open, eventDetails);
				}}
			/>
		</KeepMountedContext>
	);
}

export function DialogDropdownMenuContent(
	props: Omit<ComponentProps<typeof DropdownMenuContent>, "keepMounted">,
) {
	return <DropdownMenuContent {...props} keepMounted={useContext(KeepMountedContext)} />;
}
