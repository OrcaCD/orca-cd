import {
	getLocale,
	setLocale as setParaglideLocale,
	baseLocale,
	type Locale,
} from "@/lib/paraglide/runtime";
import { z } from "zod";

const zodLocaleLoaders: Record<Locale, () => Promise<{ default: typeof z.locales.en }>> = {
	en: () => import("zod/v4/locales/en.js"),
	de: () => import("zod/v4/locales/de.js"),
};

export async function initializeI18n() {
	const locale = getPreferredLocale();

	// Set in background to speed up initial load
	// oxlint-disable-next-line no-floating-promises
	setLocaleForLibraries(locale);

	if (locale !== getLocale()) {
		await setParaglideLocale(locale, { reload: false });
	}

	if (typeof document !== "undefined") {
		document.documentElement.lang = locale;
	}
}

export async function setLocale(locale: Locale, options: { reload?: boolean } = {}) {
	await setLocaleForLibraries(locale);
	await setParaglideLocale(locale, options);

	if (typeof document !== "undefined") {
		document.documentElement.lang = locale;
	}
}

export async function setLocaleForLibraries(locale: Locale = getLocale() || baseLocale) {
	const loadLocale = zodLocaleLoaders[locale] || zodLocaleLoaders[baseLocale];

	try {
		const { default: createLocale } = await loadLocale();
		z.config(createLocale());
	} catch (error) {
		// oxlint-disable-next-line no-console
		console.warn(`Failed to load zod locale for ${locale}:`, error);
	}
}

function getPreferredLocale(): Locale {
	return getLocale() || baseLocale;
}
