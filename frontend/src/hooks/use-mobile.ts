import * as React from "react";

const MOBILE_BREAKPOINT = 768;

const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`);

function subscribe(onChange: () => void) {
	mql.addEventListener("change", onChange);
	return () => mql.removeEventListener("change", onChange);
}

function getSnapshot() {
	return mql.matches;
}

// Read synchronously, so components don't render for desktop first and then again for mobile.
export function useIsMobile() {
	return React.useSyncExternalStore(subscribe, getSnapshot);
}
