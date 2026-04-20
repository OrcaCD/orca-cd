import {
	getLocale,
	setLocale as setParaglideLocale,
	baseLocale,
	localStorageKey,
	toLocale,
	type Locale,
} from "@/lib/paraglide/runtime";
import { z } from "zod";

export async function initializeI18n() {
	const locale = getPreferredLocale();
	await setLocaleForLibraries(locale);

	if (locale !== getLocale()) {
		await setParaglideLocale(locale, { reload: false });
	}

	if (typeof document !== "undefined") {
		document.documentElement.lang = locale;
	}
}

export async function setLocale(locale: Locale, options: { reload?: boolean } = {}) {
	if (typeof window !== "undefined") {
		localStorage.setItem(localStorageKey, locale);
	}

	await setLocaleForLibraries(locale);
	await setParaglideLocale(locale, options);

	if (typeof document !== "undefined") {
		document.documentElement.lang = locale;
	}
}

export async function setLocaleForLibraries(locale: Locale = getLocale() || baseLocale) {
	const zodResult = await import(`zod/v4/locales/${locale}.js`);

	if (zodResult.status === "fulfilled") {
		z.config(zodResult.value.default());
	} else {
		// oxlint-disable-next-line no-console
		console.warn(`Failed to load zod locale for ${locale}:`, zodResult.reason);
	}
}

function getPreferredLocale(): Locale {
	if (typeof window !== "undefined") {
		const storedLocale = toLocale(localStorage.getItem(localStorageKey));
		if (storedLocale) {
			return storedLocale;
		}
	}

	return getLocale() || baseLocale;
}
