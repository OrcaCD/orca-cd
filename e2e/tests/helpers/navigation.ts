import { expect, type Page, type Request } from "@playwright/test";

/**
 * Opens a page of the SPA and retries if the load was aborted by a network change.
 *
 * Docker adds and removes network interfaces whenever a container starts or stops, and
 * Chromium aborts all in-flight requests with ERR_NETWORK_CHANGED when it notices. If that
 * happens during a page load, some JS chunks are missing and the app never renders.
 */
export async function gotoApp(page: Page, url: string) {
	await expect(async () => {
		let networkChanged = false;
		const onRequestFailed = (request: Request) => {
			networkChanged ||= request.failure()?.errorText === "net::ERR_NETWORK_CHANGED";
		};
		page.on("requestfailed", onRequestFailed);
		try {
			await page.goto(url);
			await expect(page.locator("#app")).not.toBeEmpty({ timeout: 5_000 });
		} finally {
			page.off("requestfailed", onRequestFailed);
		}
		expect(networkChanged, `loading ${url} was aborted by a network change`).toBe(false);
	}).toPass({ timeout: 30_000 });
}
