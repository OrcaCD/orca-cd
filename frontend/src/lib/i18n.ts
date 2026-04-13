import {
	getLocale,
	setLocale as setParaglideLocale,
	baseLocale,
	type Locale,
} from "@/lib/paraglide/runtime";
import { z } from "zod";

export async function setLocale(locale: Locale) {
	await setLocaleForLibraries(locale);
	await setParaglideLocale(locale);
	document.documentElement.lang = locale;
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
