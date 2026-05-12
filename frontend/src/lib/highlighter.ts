import { createHighlighterCore } from "shiki/core";
import { createOnigurumaEngine } from "shiki/engine/oniguruma";

export const highlighter = await createHighlighterCore({
	themes: [import("shiki/themes/vitesse-light"), import("shiki/themes/vitesse-dark")],
	langs: [import("shiki/langs/yaml")],
	engine: createOnigurumaEngine(import("shiki/wasm")),
});
