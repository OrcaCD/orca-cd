import { useCallback, useState } from "react";

export type HistoryLayout = "timeline" | "table";

const HISTORY_LAYOUT_STORAGE_KEY = "orca-history-layout";
const DEFAULT_HISTORY_LAYOUT: HistoryLayout = "timeline";

export function toHistoryLayout(value: string | null | undefined): HistoryLayout | null {
	if (value === "timeline" || value === "table") {
		return value;
	}

	return null;
}

function getHistoryLayout(): HistoryLayout {
	if (typeof window === "undefined") {
		return DEFAULT_HISTORY_LAYOUT;
	}

	try {
		return (
			toHistoryLayout(window.localStorage.getItem(HISTORY_LAYOUT_STORAGE_KEY)) ??
			DEFAULT_HISTORY_LAYOUT
		);
	} catch {
		return DEFAULT_HISTORY_LAYOUT;
	}
}

export function useHistoryLayout() {
	const [historyLayout, setHistoryLayoutState] = useState<HistoryLayout>(() => getHistoryLayout());

	const setHistoryLayout = useCallback((layout: HistoryLayout) => {
		try {
			window.localStorage.setItem(HISTORY_LAYOUT_STORAGE_KEY, layout);
		} catch {
			// Ignore storage write failures and keep in-memory state.
		}
		setHistoryLayoutState(layout);
	}, []);

	return { historyLayout, setHistoryLayout };
}
