import { useHotkey, type HotkeySequence, type RegisterableHotkey } from "@tanstack/react-hotkeys";
import { useNavigate } from "@tanstack/react-router";
import { useAuth } from "./auth";
import { useTheme } from "@/components/theme-provider";
import { useThemeTransition } from "@/components/mode-toggle";
import { useCallback } from "react";

const HOTKEY_OPTIONS = {
	ignoreInputs: true,
} as const;

export const HOTKEY_SEQUENCES = {
	navigateOverview: ["G", "O"] as HotkeySequence,
	navigateApplications: ["G", "A"] as HotkeySequence,
	navigateAgents: ["G", "N"] as HotkeySequence,
	navigateRepositories: ["G", "R"] as HotkeySequence,
	navigateSettings: ["G", "S"] as HotkeySequence,
	navigateNotifications: ["G", "T"] as HotkeySequence,
	navigateAdmin: ["G", "M"] as HotkeySequence,
};

export const REGISTERABLE_HOTKEYS = {
	toggleTheme: "D" as RegisterableHotkey,
};

export function useApplicationHotkeys() {
	const navigate = useNavigate();
	const { auth } = useAuth();
	const { theme, setTheme } = useTheme();
	const { startTransition } = useThemeTransition();

	const navigateTo = useCallback(
		(to: string) => {
			void navigate({ to });
		},
		[navigate],
	);

	const toggleTheme = useCallback(() => {
		const nextTheme = theme === "dark" ? "light" : "dark";

		startTransition(() => {
			setTheme(nextTheme);
		});
	}, [theme, setTheme, startTransition]);

	const enabled = auth.isAuthenticated;

	useHotkey(HOTKEY_SEQUENCES.navigateOverview, () => navigateTo("/"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkey(HOTKEY_SEQUENCES.navigateApplications, () => navigateTo("/applications"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkey(HOTKEY_SEQUENCES.navigateAgents, () => navigateTo("/agents"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkey(HOTKEY_SEQUENCES.navigateRepositories, () => navigateTo("/repositories"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkey(HOTKEY_SEQUENCES.navigateNotifications, () => navigateTo("/notifications"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkey(HOTKEY_SEQUENCES.navigateSettings, () => navigateTo("/settings/profile"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkey(HOTKEY_SEQUENCES.navigateAdmin, () => navigateTo("/admin/system-info"), {
		...HOTKEY_OPTIONS,
		enabled: enabled && auth.isAdmin,
	});

	useHotkey(REGISTERABLE_HOTKEYS.toggleTheme, toggleTheme, {
		enabled,
		ignoreInputs: true,
		preventDefault: true,
	});
}
