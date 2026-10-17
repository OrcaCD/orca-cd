import * as React from "react";
import { createHighlighterCore, type GrammarState, type HighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";

// Add the languages you need. Each one loads the first time it is used.
const languages = {
	yaml: () => import("shiki/langs/yaml.mjs"),
};

const aliases: Record<string, keyof typeof languages> = {
	yml: "yaml",
};

// Wraps spaces and tabs so they can be made visible with CSS, see styles.css.
function renderWhitespace(content: string) {
	return content.split(/([ \t])/).map((part, index) => {
		if (part === " ") {
			return (
				<span key={index} data-whitespace="space">
					{part}
				</span>
			);
		}
		if (part === "\t") {
			return (
				<span key={index} data-whitespace="tab">
					{part}
				</span>
			);
		}
		return part;
	});
}

let highlighter: Promise<HighlighterCore> | undefined;

function getHighlighter() {
	highlighter ??= createHighlighterCore({
		themes: [import("shiki/themes/vitesse-light.mjs"), import("shiki/themes/vitesse-dark.mjs")],
		langs: [],
		engine: createJavaScriptRegexEngine({ forgiving: true }),
	});

	return highlighter;
}

function getLanguage(language: string) {
	const name = language.toLowerCase();

	if (name === "ansi") {
		return name;
	}

	if (name in languages) {
		return name as keyof typeof languages;
	}

	return aliases[name] ?? null;
}

// Loads a language and returns a highlighter for it, or null for a language
// that is not listed.
export async function loadHighlighter(language: string) {
	const lang = getLanguage(language);

	if (!lang) {
		return null;
	}

	const shiki = await getHighlighter();

	if (lang !== "ansi" && !shiki.getLoadedLanguages().includes(lang)) {
		await shiki.loadLanguage(languages[lang]);
	}

	return {
		// Returns one React node per line, and the grammar state after the last
		// line. Pass that state back to continue where the code left off.
		highlight(code: string, state?: GrammarState) {
			const tokenize = (text: string) =>
				shiki.codeToTokens(text, {
					lang,
					themes: { light: "vitesse-light", dark: "vitesse-dark" },
					defaultColor: false,
					grammarState: state,
				}).tokens;

			// ANSI has no grammar state, so each line is tokenized on its own and
			// gets the same colors whether it arrives in one piece or streams.
			const tokens =
				lang === "ansi" ? code.split("\n").flatMap((line) => tokenize(line)) : tokenize(code);

			return {
				lines: tokens.map((line) =>
					line.map((token, index) => (
						<span
							key={index}
							style={token.htmlStyle as React.CSSProperties}
							className="text-(--shiki-light) dark:text-(--shiki-dark)"
						>
							{renderWhitespace(token.content)}
						</span>
					)),
				),
				state: shiki.getLastGrammarState(tokens),
			};
		},
	};
}
