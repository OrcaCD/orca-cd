import { useCallback, useEffect, useState } from "react";

export type PreferredLayout = "grid" | "table";

const LAYOUT_STORAGE_KEY = "orca-preferred-layout";
const DEFAULT_LAYOUT: PreferredLayout = "grid";

export function toPreferredLayout(value: string | null | undefined): PreferredLayout | null {
	if (value === "grid" || value === "table") {
		return value;
	}

	return null;
}

export function getPreferredLayout(): PreferredLayout {
	if (typeof window === "undefined") {
		return DEFAULT_LAYOUT;
	}

	try {
		return toPreferredLayout(window.localStorage.getItem(LAYOUT_STORAGE_KEY)) ?? DEFAULT_LAYOUT;
	} catch {
		return DEFAULT_LAYOUT;
	}
}

export function setPreferredLayout(layout: PreferredLayout): void {
	if (typeof window === "undefined") {
		return;
	}

	try {
		window.localStorage.setItem(LAYOUT_STORAGE_KEY, layout);
	} catch {
		// Ignore storage write failures and keep in-memory state.
	}
}

export function usePreferredLayout() {
	const [preferredLayout, setPreferredLayoutState] = useState<PreferredLayout>(() =>
		getPreferredLayout(),
	);

	useEffect(() => {
		if (typeof window === "undefined") {
			return;
		}

		const onStorage = (event: StorageEvent) => {
			if (event.key !== LAYOUT_STORAGE_KEY) {
				return;
			}

			const nextLayout = toPreferredLayout(event.newValue);
			if (nextLayout) {
				setPreferredLayoutState(nextLayout);
			}
		};

		window.addEventListener("storage", onStorage);
		return () => window.removeEventListener("storage", onStorage);
	}, []);

	const updatePreferredLayout = useCallback((layout: PreferredLayout) => {
		setPreferredLayout(layout);
		setPreferredLayoutState(layout);
	}, []);

	return {
		preferredLayout,
		setPreferredLayout: updatePreferredLayout,
	};
}
