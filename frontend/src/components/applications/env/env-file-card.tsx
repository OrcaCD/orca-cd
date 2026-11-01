import { Braces, Lock, Trash2, Undo2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { FieldError } from "@/components/ui/field";
import { Separator } from "@/components/ui/separator";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
	MAX_ENV_KEYS_PER_FILE,
	envValueErrorMessage,
	type EnvDraft,
	type EnvEntry,
	type EnvFile,
	type EnvRow,
} from "@/lib/application-env";
import { cn } from "@/lib/utils";
import { m } from "@/lib/paraglide/messages";
import { AddEnvEntryDialog } from "./add-env-entry-dialog";
import { ReplaceEnvSecretDialog } from "./replace-env-secret-dialog";

export function EnvFileCard({
	file,
	rows,
	disabled,
	onDraftChange,
}: {
	file: EnvFile;
	rows: EnvRow[];
	disabled: boolean;
	onDraftChange: (key: string, entry: EnvEntry | undefined, draft: EnvDraft | null) => void;
}) {
	const entries = new Map(file.entries.map((entry) => [entry.key, entry]));
	const activeRows = rows.filter((row) => !row.deleted).length;

	return (
		<Card>
			<CardHeader>
				<CardTitle className="font-mono">{file.path}</CardTitle>
				<CardDescription className="flex flex-wrap items-center gap-1.5">
					<span>{m.envUsedBy()}</span>
					{file.services.length === 0 ? (
						<span>{m.envNoServices()}</span>
					) : (
						file.services.map((service) => (
							<Badge key={service} variant="secondary" className="font-mono">
								{service}
							</Badge>
						))
					)}
				</CardDescription>
				<CardAction>
					<AddEnvEntryDialog
						path={file.path}
						existingKeys={new Set(rows.map((row) => row.key))}
						disabled={disabled || activeRows >= MAX_ENV_KEYS_PER_FILE}
						onAdd={(key, value, isSecret) =>
							onDraftChange(key, undefined, { delete: false, value, isSecret })
						}
					/>
				</CardAction>
			</CardHeader>
			<Separator />
			{activeRows >= MAX_ENV_KEYS_PER_FILE && (
				<p className="px-(--card-spacing) text-muted-foreground">{m.envErrorTooManyKeys()}</p>
			)}
			{rows.length === 0 ? (
				<p className="px-(--card-spacing) text-muted-foreground">{m.envEmptyFile()}</p>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead className="pl-(--card-spacing)">{m.envColumnKey()}</TableHead>
							<TableHead className="w-full">{m.envColumnValue()}</TableHead>
							<TableHead className="pr-(--card-spacing)" />
						</TableRow>
					</TableHeader>
					<TableBody>
						{rows.map((row) => (
							<EnvRowItem
								key={row.key}
								row={row}
								disabled={disabled}
								onChange={(draft) => onDraftChange(row.key, entries.get(row.key), draft)}
							/>
						))}
					</TableBody>
				</Table>
			)}
		</Card>
	);
}

function EnvRowItem({
	row,
	disabled,
	onChange,
}: {
	row: EnvRow;
	disabled: boolean;
	onChange: (draft: EnvDraft | null) => void;
}) {
	const valueError = row.valueError;
	const canUndo = !!row.draft && !row.isNew;

	return (
		<TableRow className="align-top">
			<TableCell className="max-w-72 pl-(--card-spacing) font-mono">
				<div className="flex h-8 items-center gap-2">
					{row.isSecret ? (
						<Lock className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
					) : (
						<Braces className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
					)}
					<span
						title={row.key}
						className={cn("min-w-0 truncate", row.deleted && "text-muted-foreground line-through")}
					>
						{row.key}
					</span>
					{row.deleted ? (
						<Badge variant="destructive">{m.envBadgeDeleted()}</Badge>
					) : row.isNew ? (
						<Badge variant="success">{m.envBadgeNew()}</Badge>
					) : row.draft ? (
						<Badge variant="secondary">{m.envBadgeChanged()}</Badge>
					) : null}
				</div>
			</TableCell>
			<TableCell className="whitespace-normal">
				{row.deleted ? null : row.isSecret ? (
					<div className="flex h-8 items-center font-mono text-muted-foreground">
						<span aria-hidden>••••••••</span>
						<span className="sr-only">{m.envSecretIsSet()}</span>
					</div>
				) : (
					<>
						<Textarea
							className="min-h-8 resize-none py-1 font-mono"
							rows={1}
							value={row.value}
							disabled={disabled}
							aria-label={row.key}
							aria-invalid={!!valueError}
							autoComplete="off"
							spellCheck={false}
							onChange={(e) => onChange({ delete: false, value: e.target.value, isSecret: false })}
						/>
						{valueError && (
							<FieldError className="mt-1">{envValueErrorMessage(valueError)}</FieldError>
						)}
					</>
				)}
			</TableCell>
			<TableCell className="pr-(--card-spacing)">
				<div className="flex items-center justify-end gap-1">
					{row.isSecret && !row.deleted && (
						<ReplaceEnvSecretDialog
							envKey={row.key}
							disabled={disabled}
							onReplace={(value) => onChange({ delete: false, value, isSecret: true })}
						/>
					)}
					{!row.isSecret && !row.deleted && (
						<IconAction
							label={m.envMakeSecret()}
							disabled={disabled || !!valueError}
							onClick={() => onChange({ delete: false, value: row.value, isSecret: true })}
						>
							<Lock />
						</IconAction>
					)}
					{canUndo && (
						<IconAction label={m.envUndo()} disabled={disabled} onClick={() => onChange(null)}>
							<Undo2 />
						</IconAction>
					)}
					{!row.deleted && (
						<IconAction
							label={m.envDeleteEntry({ key: row.key })}
							disabled={disabled}
							onClick={() => onChange(row.isNew ? null : { delete: true })}
						>
							<Trash2 />
						</IconAction>
					)}
				</div>
			</TableCell>
		</TableRow>
	);
}

function IconAction({
	label,
	disabled,
	onClick,
	children,
}: {
	label: string;
	disabled: boolean;
	onClick: () => void;
	children: React.ReactNode;
}) {
	return (
		<Tooltip>
			<TooltipTrigger
				render={
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label={label}
						disabled={disabled}
						onClick={onClick}
					/>
				}
			>
				{children}
			</TooltipTrigger>
			<TooltipContent>{label}</TooltipContent>
		</Tooltip>
	);
}
