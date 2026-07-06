import { createHighlighterCore } from "shiki/core";
import { createOnigurumaEngine } from "shiki/engine/oniguruma";

export const highlighter = await createHighlighterCore({
	themes: [import("@shikijs/themes/vitesse-light"), import("@shikijs/themes/vitesse-dark")],
	langs: [import("@shikijs/langs/yaml")],
	engine: createOnigurumaEngine(import("shiki/wasm")),
});
