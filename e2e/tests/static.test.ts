import { test, expect, type APIRequestContext } from "@playwright/test";

test.use({ storageState: { cookies: [], origins: [] } });

const encodings = ["gzip", "br", "zstd"];

const uncompressed = "identity";

async function getAssetPath(request: APIRequestContext): Promise<string> {
	const response = await request.get("/", { headers: { "Accept-Encoding": uncompressed } });
	const match = /src="(\/assets\/[^"]+\.js)"/.exec(await response.text());
	expect(match, "index.html references a script").not.toBeNull();
	return match![1];
}

test.describe("SPA routes serve index.html", () => {
	for (const path of ["/", "/login", "/applications", "/some/unknown/route"]) {
		test(`${path} without compression`, async ({ request }) => {
			const response = await request.get(path, {
				headers: { "Accept-Encoding": uncompressed },
				maxRedirects: 0,
			});
			expect(response.status()).toBe(200);
			expect(response.headers()["content-type"]).toContain("text/html");
			expect(response.headers()["content-encoding"]).toBeUndefined();
			expect(response.headers()["cache-control"]).toBe("no-store");
			expect((await response.text()).toLowerCase()).toContain("<!doctype html");
		});

		for (const encoding of encodings) {
			test(`${path} with ${encoding}`, async ({ request }) => {
				const response = await request.get(path, {
					headers: { "Accept-Encoding": encoding },
					maxRedirects: 0,
				});
				expect(response.status()).toBe(200);
				expect(response.headers()["content-type"]).toContain("text/html");
				expect(response.headers()["content-encoding"]).toBe(encoding);
				expect(response.headers()["vary"]).toContain("Accept-Encoding");
				expect(response.headers()["cache-control"]).toBe("no-store");
			});
		}
	}
});

test.describe("explicit index.html is normalized", () => {
	for (const encoding of [uncompressed, ...encodings]) {
		test(`redirects with ${encoding}`, async ({ request }) => {
			const response = await request.get("/index.html", {
				headers: { "Accept-Encoding": encoding },
				maxRedirects: 0,
			});
			expect(response.status()).toBe(301);
			expect(response.headers()["location"]).toBe("/");
		});
	}

	test("keeps the query string", async ({ request }) => {
		const response = await request.get("/index.html?a=1", { maxRedirects: 0 });
		expect(response.status()).toBe(301);
		expect(response.headers()["location"]).toBe("/?a=1");
	});
});

test.describe("static assets", () => {
	test("are served without compression", async ({ request }) => {
		const asset = await getAssetPath(request);
		const response = await request.get(asset, { headers: { "Accept-Encoding": uncompressed } });
		expect(response.status()).toBe(200);
		expect(response.headers()["content-type"]).toContain("javascript");
		expect(response.headers()["content-encoding"]).toBeUndefined();
		expect(response.headers()["cache-control"]).toBe("public, max-age=86400");
	});

	for (const encoding of encodings) {
		test(`are served pre-compressed with ${encoding}`, async ({ request }) => {
			const asset = await getAssetPath(request);
			const response = await request.get(asset, { headers: { "Accept-Encoding": encoding } });
			expect(response.status()).toBe(200);
			expect(response.headers()["content-type"]).toContain("javascript");
			expect(response.headers()["content-encoding"]).toBe(encoding);
			expect(response.headers()["vary"]).toContain("Accept-Encoding");
		});
	}

	test("return 304 when unchanged", async ({ request }) => {
		const asset = await getAssetPath(request);
		const first = await request.get(asset);
		const lastModified = first.headers()["last-modified"];
		expect(lastModified).toBeTruthy();

		const second = await request.get(asset, { headers: { "If-Modified-Since": lastModified } });
		expect(second.status()).toBe(304);
	});
});

test.describe("route normalization", () => {
	test("trailing slashes are removed", async ({ request }) => {
		const response = await request.get("/login/?a=1", { maxRedirects: 0 });
		expect(response.status()).toBe(302);
		expect(response.headers()["location"]).toBe("/login?a=1");
	});

	test("unknown API routes return 404 instead of the SPA", async ({ request }) => {
		const response = await request.get("/api/v1/does-not-exist", { maxRedirects: 0 });
		expect(response.status()).toBe(404);
		expect(response.headers()["content-type"]).toContain("application/json");
	});
});
