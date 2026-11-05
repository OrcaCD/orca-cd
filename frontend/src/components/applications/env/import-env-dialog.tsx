import { useRef, useState } from "react";
import { FileUp } from "lucide-react";
import {
	ResponsiveDialog,
	ResponsiveDialogContent,
	ResponsiveDialogDescription,
	ResponsiveDialogFooter,
	ResponsiveDialogHeader,
	ResponsiveDialogTitle,
} from "@/components/responsive-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { FieldError } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import {
	MAX_ENV_FILE_BYTES,
	MAX_ENV_KEYS_PER_FILE,
	buildEnvImportRows,
	envImportErrorMessage,
	envImportStatus,
	parseEnvFile,
	type EnvEntry,
	type EnvImportRow,
	type EnvImportStatus,
	type ParsedEnvEntry,
} from "@/lib/application-env";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

interface ParsedImport {
	rows: EnvImportRow[];
	ignoredLines: number;
}

function toggled(set: ReadonlySet<string>, key: string, on: boolean): Set<string> {
	const next = new Set(set);
	if (on) {
		next.add(key);
	} else {
		next.delete(key);
	}
	return next;
}

const overwriteBadgeClass =
	"border-amber-500/30 bg-amber-500/20 text-amber-600 dark:text-amber-400";

function StatusBadge({ status }: { status: EnvImportStatus }) {
	switch (status) {
		case "new":
			return <Badge variant="success">{m.envBadgeNew()}</Badge>;
		case "changed":
			return (
				<Badge variant="outline" className={overwriteBadgeClass}>
					{m.envBadgeChanged()}
				</Badge>
			);
		case "replacesSecret":
			return (
				<Badge variant="outline" className={overwriteBadgeClass}>
					{m.envImportStatusReplacesSecret()}
				</Badge>
			);
		case "unchanged":
			return <Badge variant="outline">{m.envImportStatusUnchanged()}</Badge>;
	}
}

export function ImportEnvDialog({
	entries,
	activeKeys,
	disabled,
	onImport,
}: {
	entries: EnvEntry[];
	activeKeys: ReadonlySet<string>;
	disabled: boolean;
	onImport: (items: ParsedEnvEntry[]) => void;
}) {
	const [open, setOpen] = useState(false);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [parsed, setParsed] = useState<ParsedImport | null>(null);
	const [included, setIncluded] = useState<ReadonlySet<string>>(new Set());
	const [secrets, setSecrets] = useState<ReadonlySet<string>>(new Set());
	const fileInput = useRef<HTMLInputElement>(null);

	const rows = parsed?.rows ?? [];
	const selectableCount = rows.filter((row) => !row.error).length;
	const selectedRows = rows.filter((row) => included.has(row.key));
	const addedKeys = selectedRows.filter((row) => !activeKeys.has(row.key)).length;
	const tooManyKeys = activeKeys.size + addedKeys > MAX_ENV_KEYS_PER_FILE;

	function reset() {
		setLoading(false);
		setError(null);
		setParsed(null);
		setIncluded(new Set());
		setSecrets(new Set());
	}

	function handleOpenChange(nextOpen: boolean) {
		setOpen(nextOpen);
		if (!nextOpen) {
			reset();
		}
	}

	async function loadContent(content: string) {
		if (content.trim() === "") {
			setError(m.envImportErrorNoEntries());
			return;
		}

		setLoading(true);
		setError(null);
		try {
			const result = await parseEnvFile(content);
			if (result.entries.length === 0) {
				setError(m.envImportErrorNoEntries());
				return;
			}
			const importRows = buildEnvImportRows(entries, result.entries);
			setParsed({ rows: importRows, ignoredLines: result.ignoredLines });
			setIncluded(
				new Set(
					importRows
						.filter((row) => !row.error && envImportStatus(row, row.isSecret) !== "unchanged")
						.map((row) => row.key),
				),
			);
			setSecrets(new Set(importRows.filter((row) => row.isSecret).map((row) => row.key)));
		} catch (err) {
			setError(err instanceof Error && err.message ? err.message : m.envImportErrorParse());
		} finally {
			setLoading(false);
		}
	}

	async function loadFile(file: File) {
		if (file.size > MAX_ENV_FILE_BYTES) {
			setError(m.envImportErrorTooLarge());
			return;
		}
		let content: string;
		try {
			content = await file.text();
		} catch {
			setError(m.envImportErrorParse());
			return;
		}
		await loadContent(content);
	}

	function handlePaste(e: React.ClipboardEvent) {
		if (parsed || loading) {
			return;
		}
		const file = e.clipboardData.files[0];
		if (file) {
			e.preventDefault();
			void loadFile(file);
			return;
		}
		const text = e.clipboardData.getData("text");
		if (text) {
			e.preventDefault();
			void loadContent(text);
		}
	}

	function handleDrop(e: React.DragEvent) {
		e.preventDefault();
		const file = e.dataTransfer.files[0];
		if (file && !loading) {
			void loadFile(file);
		}
	}

	function handleConfirm() {
		onImport(
			selectedRows.map((row) => ({
				key: row.key,
				value: row.value,
				isSecret: secrets.has(row.key),
			})),
		);
		handleOpenChange(false);
	}

	return (
		<>
			<Button variant="outline" size="sm" disabled={disabled} onClick={() => setOpen(true)}>
				<FileUp />
				{m.envImportButton()}
			</Button>
			<ResponsiveDialog open={open} onOpenChange={handleOpenChange}>
				<ResponsiveDialogContent
					className={cn(
						"max-h-[calc(100dvh-2rem)] overflow-y-auto",
						parsed ? "sm:max-w-3xl" : "sm:max-w-lg",
					)}
					onPaste={handlePaste}
				>
					<ResponsiveDialogHeader>
						<ResponsiveDialogTitle>{m.envImportTitle()}</ResponsiveDialogTitle>
						<ResponsiveDialogDescription>{m.envImportDescription()}</ResponsiveDialogDescription>
					</ResponsiveDialogHeader>

					{!parsed ? (
						<div
							className="flex flex-col items-center gap-3 rounded-lg border border-dashed p-6 text-center"
							onDragOver={(e) => e.preventDefault()}
							onDrop={handleDrop}
						>
							<p className="text-muted-foreground">{m.envImportDropHint()}</p>
							<div className="flex flex-wrap justify-center gap-2">
								<Button
									type="button"
									variant="outline"
									disabled={loading}
									onClick={() => fileInput.current?.click()}
								>
									<FileUp />
									{m.envImportChooseFile()}
								</Button>
							</div>
							<input
								ref={fileInput}
								id="env-import-file"
								type="file"
								className="hidden"
								onChange={(e) => {
									const file = e.target.files?.[0];
									e.target.value = "";
									if (file) {
										void loadFile(file);
									}
								}}
							/>
							{error && <FieldError>{error}</FieldError>}
						</div>
					) : (
						<>
							<div className="max-h-[40vh] overflow-auto rounded-lg border sm:max-h-[50vh]">
								<Table>
									<TableHeader>
										<TableRow>
											<TableHead className="w-8 [&:has([role=checkbox])]:pr-1">
												<Checkbox
													aria-label={m.envImportSelectAll()}
													checked={selectableCount > 0 && included.size === selectableCount}
													indeterminate={included.size > 0 && included.size < selectableCount}
													disabled={selectableCount === 0}
													onCheckedChange={(checked) =>
														setIncluded(
															checked
																? new Set(rows.filter((row) => !row.error).map((row) => row.key))
																: new Set(),
														)
													}
												/>
											</TableHead>
											<TableHead className="px-2 sm:px-4">{m.envColumnKey()}</TableHead>
											<TableHead className="px-2 sm:px-4">{m.envImportColumnSecret()}</TableHead>
											<TableHead className="w-full px-2 sm:px-4">{m.envColumnValue()}</TableHead>
										</TableRow>
									</TableHeader>
									<TableBody>
										{rows.map((row) => {
											const isSecret = secrets.has(row.key);
											const keyError =
												row.error === "empty" || row.error === "invalid" ? row.error : null;
											const valueError = row.error && !keyError ? row.error : null;
											return (
												<TableRow key={row.key} className="align-top">
													<TableCell className="[&:has([role=checkbox])]:pr-1">
														<Checkbox
															aria-label={m.envImportSelectRow({ key: row.key })}
															checked={included.has(row.key)}
															disabled={!!row.error}
															onCheckedChange={(checked) =>
																setIncluded((cur) => toggled(cur, row.key, checked))
															}
														/>
													</TableCell>
													<TableCell className="max-w-40 px-2 font-mono sm:max-w-64 sm:px-4">
														<div className="flex items-center gap-2">
															<span title={row.key} className="min-w-0 truncate">
																{row.key}
															</span>
															{!row.error && (
																<StatusBadge status={envImportStatus(row, isSecret)} />
															)}
														</div>
														{keyError && (
															<p className="font-sans text-xs text-destructive">
																{envImportErrorMessage(keyError)}
															</p>
														)}
													</TableCell>
													<TableCell className="px-2 sm:px-4">
														<Switch
															aria-label={m.envSecretSwitchLabel()}
															checked={isSecret}
															disabled={row.secretLocked || !!row.error}
															onCheckedChange={(checked) =>
																setSecrets((cur) => toggled(cur, row.key, checked))
															}
														/>
													</TableCell>
													<TableCell className="max-w-40 px-2 font-mono sm:max-w-64 sm:px-4">
														{valueError ? (
															<p className="font-sans text-xs text-destructive">
																{envImportErrorMessage(valueError)}
															</p>
														) : isSecret ? (
															<>
																<span aria-hidden className="text-muted-foreground">
																	••••••••
																</span>
																<span className="sr-only">{m.envSecretIsSet()}</span>
															</>
														) : (
															<span title={row.value} className="block truncate">
																{row.value}
															</span>
														)}
													</TableCell>
												</TableRow>
											);
										})}
									</TableBody>
								</Table>
							</div>

							<div className="flex flex-col gap-1 text-xs text-muted-foreground">
								<p>{m.envImportSelected({ selected: selectedRows.length, total: rows.length })}</p>
								<p>{m.envImportMergeNote()}</p>
								{parsed.ignoredLines > 0 && (
									<p>{m.envImportIgnoredLines({ count: parsed.ignoredLines })}</p>
								)}
								{tooManyKeys && <FieldError>{m.envErrorTooManyKeys()}</FieldError>}
							</div>
						</>
					)}

					<ResponsiveDialogFooter>
						<Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
							{m.cancel()}
						</Button>
						{parsed && (
							<Button
								type="button"
								disabled={selectedRows.length === 0 || tooManyKeys}
								onClick={handleConfirm}
							>
								{m.envImportConfirm()}
							</Button>
						)}
					</ResponsiveDialogFooter>
				</ResponsiveDialogContent>
			</ResponsiveDialog>
		</>
	);
}
