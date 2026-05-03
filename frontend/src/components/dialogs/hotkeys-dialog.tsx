import {
	useHotkey,
	useHotkeySequence,
	type HotkeySequence,
	type RegisterableHotkey,
} from "@tanstack/react-hotkeys";
import { useNavigate } from "@tanstack/react-router";
import { Fragment, useCallback } from "react";
import { useAuth } from "@/lib/auth";
import { useTheme } from "../theme-provider";
import { useThemeTransition } from "../mode-toggle";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "../ui/button";
import { Keyboard } from "lucide-react";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { m } from "@/lib/paraglide/messages";

const HOTKEY_OPTIONS = {
	ignoreInputs: true,
} as const;

const HOTKEY_SEQUENCES = {
	navigateApplications: ["G", "A"] as HotkeySequence,
	navigateAgents: ["G", "N"] as HotkeySequence,
	navigateRepositories: ["G", "R"] as HotkeySequence,
	navigateSettings: ["G", "S"] as HotkeySequence,
	navigateAdmin: ["G", "M"] as HotkeySequence,
	toggleTheme: "Mod+Shift+M" as RegisterableHotkey,
};

export default function HotkeysDialog() {
	const navigate = useNavigate();
	const { auth } = useAuth();
	const { theme, setTheme } = useTheme();
	const { startTransition } = useThemeTransition();
	const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform);
	const modKey = isMac ? "Cmd" : "Ctrl";

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

	const navigationShortcuts = [
		{
			id: "applications",
			label: m.hotkeysGoTo({ target: m.navApplications() }),
			sequence: HOTKEY_SEQUENCES.navigateApplications,
		},
		{
			id: "agents",
			label: m.hotkeysGoTo({ target: m.navAgents() }),
			sequence: HOTKEY_SEQUENCES.navigateAgents,
		},
		{
			id: "repositories",
			label: m.hotkeysGoTo({ target: m.navRepositories() }),
			sequence: HOTKEY_SEQUENCES.navigateRepositories,
		},
		{
			id: "settings",
			label: m.hotkeysGoTo({ target: m.settings() }),
			sequence: HOTKEY_SEQUENCES.navigateSettings,
		},
		...(auth.isAdmin
			? [
					{
						id: "admin",
						label: m.hotkeysGoTo({ target: m.navAdmin() }),
						sequence: HOTKEY_SEQUENCES.navigateAdmin,
					},
				]
			: []),
	] as const;
	const themeToggleKeys = [modKey, "Shift", "M"] as const;

	useHotkeySequence(HOTKEY_SEQUENCES.navigateApplications, () => navigateTo("/applications"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkeySequence(HOTKEY_SEQUENCES.navigateAgents, () => navigateTo("/agents"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkeySequence(HOTKEY_SEQUENCES.navigateRepositories, () => navigateTo("/repositories"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkeySequence(HOTKEY_SEQUENCES.navigateSettings, () => navigateTo("/settings/profile"), {
		...HOTKEY_OPTIONS,
		enabled,
	});

	useHotkeySequence(HOTKEY_SEQUENCES.navigateAdmin, () => navigateTo("/admin/system-info"), {
		...HOTKEY_OPTIONS,
		enabled: enabled && auth.isAdmin,
	});

	useHotkey(HOTKEY_SEQUENCES.toggleTheme, toggleTheme, {
		enabled,
		ignoreInputs: true,
		preventDefault: true,
	});

	return (
		<Dialog>
			<DialogTrigger asChild>
				<Button
					variant="ghost"
					size="icon"
					className="hidden sm:flex cursor-pointer"
					aria-label={m.hotkeysOpenDialog()}
				>
					<Keyboard className="h-5 w-5 text-muted-foreground" />
					<span className="sr-only">{m.hotkeysOpenDialog()}</span>
				</Button>
			</DialogTrigger>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">{m.hotkeysDialogTitle()}</DialogTitle>
					<DialogDescription>{m.hotkeysDialogDescription()}</DialogDescription>
				</DialogHeader>

				<p>{m.hotkeysNavigationShortcuts()}</p>

				<ul className="space-y-2">
					{navigationShortcuts.map((shortcut) => (
						<li
							key={shortcut.id}
							className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"
						>
							<span className="text-sm text-foreground">{shortcut.label}</span>
							<KbdGroup>
								{shortcut.sequence.map((key, index) => (
									<Fragment key={`${shortcut.id}-${key}`}>
										{index > 0 && (
											<span className="text-xs text-muted-foreground">{m.hotkeysThen()}</span>
										)}
										<Kbd>{key}</Kbd>
									</Fragment>
								))}
							</KbdGroup>
						</li>
					))}
				</ul>

				<p className="mt-4">{m.hotkeysOtherShortcuts()}</p>

				<ul className="space-y-2">
					<li className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
						<span className="text-sm text-foreground">{m.hotkeysToggleTheme()}</span>
						<KbdGroup>
							{themeToggleKeys.map((key, index) => (
								<Fragment key={`toggle-theme-${key}`}>
									{index > 0 && <span className="text-xs text-muted-foreground">+</span>}
									<Kbd>{key}</Kbd>
								</Fragment>
							))}
						</KbdGroup>
					</li>
				</ul>
			</DialogContent>
		</Dialog>
	);
}
