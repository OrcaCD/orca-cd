import { diffArrays } from "diff";
import { ChevronDown, ChevronsUpDown, ChevronUp } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { CodeBlockLine } from "@/components/ui/code-block";
import { m } from "@/lib/paraglide/messages";

// Unchanged lines shown around every change, and lines revealed per expand click.
const CONTEXT_LINES = 3;
const EXPAND_STEP = 20;

interface ComposeDiffLine {
	variant: "default" | "added" | "removed";
	// Line indexes in the previous and current file. A line missing on one side holds
	// the index the next line on that side gets.
	previousIndex: number;
	currentIndex: number;
}

export interface ComposeDiff {
	lines: ComposeDiffLine[];
	additions: number;
	deletions: number;
}

function splitComposeLines(composeFile: string): string[] {
	if (composeFile.length === 0) {
		return [];
	}

	const lines = composeFile.split("\n");
	if (lines.at(-1) === "") {
		lines.pop();
	}

	return lines;
}

export function buildComposeDiff(previousComposeFile: string, composeFile: string): ComposeDiff {
	const lines: ComposeDiffLine[] = [];
	let previousIndex = 0;
	let currentIndex = 0;

	for (const change of diffArrays(
		splitComposeLines(previousComposeFile),
		splitComposeLines(composeFile),
	)) {
		for (let i = 0; i < change.value.length; i++) {
			if (change.removed) {
				lines.push({ variant: "removed", previousIndex: previousIndex++, currentIndex });
			} else if (change.added) {
				lines.push({ variant: "added", previousIndex, currentIndex: currentIndex++ });
			} else {
				lines.push({
					variant: "default",
					previousIndex: previousIndex++,
					currentIndex: currentIndex++,
				});
			}
		}
	}

	return {
		lines,
		additions: lines.filter((line) => line.variant === "added").length,
		deletions: lines.filter((line) => line.variant === "removed").length,
	};
}

type DiffSegment =
	| { type: "line"; index: number }
	// Hidden unchanged lines in [start, end).
	| { type: "gap"; start: number; end: number };

function buildDiffSegments(lines: ComposeDiffLine[], revealed: ReadonlySet<number>): DiffSegment[] {
	const visible = lines.map((_, index) => revealed.has(index));
	lines.forEach((line, index) => {
		if (line.variant === "default") {
			return;
		}
		const end = Math.min(lines.length, index + CONTEXT_LINES + 1);
		for (let i = Math.max(0, index - CONTEXT_LINES); i < end; i++) {
			visible[i] = true;
		}
	});

	const segments: DiffSegment[] = [];
	for (let index = 0; index < lines.length; index++) {
		if (visible[index]) {
			segments.push({ type: "line", index });
			continue;
		}
		const last = segments.at(-1);
		if (last?.type === "gap") {
			last.end = index + 1;
		} else {
			segments.push({ type: "gap", start: index, end: index + 1 });
		}
	}

	return segments;
}

// hunkHeader describes the visible lines starting at start like git does, e.g. "@@ -37,6 +37,7 @@".
function hunkHeader(lines: ComposeDiffLine[], segments: DiffSegment[], start: number): string {
	const first = lines[start];
	let previousCount = 0;
	let currentCount = 0;
	for (const segment of segments) {
		if (segment.type === "gap") {
			if (segment.start > start) {
				break;
			}
			continue;
		}
		if (segment.index < start) {
			continue;
		}
		const line = lines[segment.index];
		if (line.variant !== "added") {
			previousCount++;
		}
		if (line.variant !== "removed") {
			currentCount++;
		}
	}

	const range = (index: number, count: number) => `${count === 0 ? index : index + 1},${count}`;
	return `@@ -${range(first.previousIndex, previousCount)} +${range(first.currentIndex, currentCount)} @@`;
}

export function ComposeDiffLines({
	diff,
	previousLines,
	currentLines,
}: {
	diff: ComposeDiff;
	previousLines: React.ReactNode[];
	currentLines: React.ReactNode[];
}) {
	// Revealed lines belong to one diff and reset once the compose file changes.
	const [expanded, setExpanded] = useState<{ diff: ComposeDiff; revealed: Set<number> }>({
		diff,
		revealed: new Set(),
	});
	const revealed = expanded.diff === diff ? expanded.revealed : new Set<number>();

	if (diff.additions === 0 && diff.deletions === 0) {
		return (
			<span className="col-span-2 font-sans text-muted-foreground">{m.composeDiffNoChanges()}</span>
		);
	}

	const segments = buildDiffSegments(diff.lines, revealed);

	const reveal = (start: number, end: number) => {
		const next = new Set(revealed);
		for (let i = start; i < end; i++) {
			next.add(i);
		}
		setExpanded({ diff, revealed: next });
	};

	return segments.map((segment) => {
		if (segment.type === "line") {
			const line = diff.lines[segment.index];
			const removed = line.variant === "removed";
			const lineIndex = removed ? line.previousIndex : line.currentIndex;
			return (
				<CodeBlockLine
					key={segment.index}
					variant={line.variant}
					// Hidden lines are skipped, so every line sets its own number.
					style={{ counterSet: `line ${lineIndex + 1}` }}
					className={
						line.variant === "added"
							? "bg-green-500/15 after:text-green-600 dark:after:text-green-400"
							: undefined
					}
				>
					{removed ? previousLines[lineIndex] : currentLines[lineIndex]}
				</CodeBlockLine>
			);
		}

		const { start, end } = segment;
		const atTop = start === 0;
		const atBottom = end === diff.lines.length;
		const expandUp = () => reveal(Math.max(start, end - EXPAND_STEP), end);
		const expandDown = () => reveal(start, Math.min(end, start + EXPAND_STEP));

		return (
			<span
				key={`gap-${start}`}
				className="relative col-span-2 -mx-3 grid grid-cols-subgrid items-center gap-x-3 bg-muted/60 px-3 font-sans text-xs text-muted-foreground select-none"
			>
				<span className="flex min-w-6 flex-col items-end">
					{!atTop && (atBottom || end - start > EXPAND_STEP) && (
						<ExpandButton label={m.composeDiffExpandDown()} onClick={expandDown}>
							<ChevronDown />
						</ExpandButton>
					)}
					{!atBottom && (atTop || end - start > EXPAND_STEP) && (
						<ExpandButton label={m.composeDiffExpandUp()} onClick={expandUp}>
							<ChevronUp />
						</ExpandButton>
					)}
					{!atTop && !atBottom && end - start <= EXPAND_STEP && (
						<ExpandButton label={m.composeDiffExpandAll()} onClick={() => reveal(start, end)}>
							<ChevronsUpDown />
						</ExpandButton>
					)}
				</span>
				<span className="min-w-0 truncate font-mono">
					{atBottom ? null : hunkHeader(diff.lines, segments, end)}
				</span>
			</span>
		);
	});
}

function ExpandButton({
	label,
	onClick,
	children,
}: {
	label: string;
	onClick: () => void;
	children: React.ReactNode;
}) {
	return (
		<Button
			variant="ghost"
			size="icon-xs"
			aria-label={label}
			title={label}
			className="hover:bg-primary/15 hover:text-primary"
			onClick={onClick}
		>
			{children}
		</Button>
	);
}
