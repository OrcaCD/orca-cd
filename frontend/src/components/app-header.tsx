import { useLayoutEffect, useRef } from "react";
import DemoBanner from "@/components/demo-banner";
import Navbar from "@/components/navbar";

/**
 * Sticky app chrome (demo banner + navbar). Its rendered height is published as
 * `--app-header-height` so fixed elements such as the page sidebars start right below it, no
 * matter whether the banner is shown or wraps onto several lines.
 */
export function AppHeader() {
	const ref = useRef<HTMLDivElement>(null);

	useLayoutEffect(() => {
		const element = ref.current;
		if (!element) {
			return;
		}
		const root = document.documentElement;
		const update = () =>
			root.style.setProperty("--app-header-height", `${element.getBoundingClientRect().height}px`);
		update();
		const observer = new ResizeObserver(update);
		observer.observe(element);
		return () => {
			observer.disconnect();
			root.style.removeProperty("--app-header-height");
		};
	}, []);

	return (
		<div ref={ref} className="sticky top-0 z-50">
			<DemoBanner />
			<Navbar />
		</div>
	);
}
