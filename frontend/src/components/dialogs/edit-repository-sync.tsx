// oxlint-disable react/no-children-prop
import { InfoIcon, RefreshCwIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { updateRepository, type Repository, type RepositorySyncType } from "@/lib/repositories";
import {
	RepositoryDialogLoadingOverlay,
	SyncTypeRadioGroup,
	WebhookSetupDetails,
} from "./repository-shared";
import SuccessAlert from "@/components/alerts/success-alert";
import { m } from "@/lib/paraglide/messages";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const syncSchema = z.object({
	syncType: z.enum(["webhook", "polling", "manual"]),
	pollingIntervalSeconds: z.number().int(),
	githubActionsOIDCEnabled: z.boolean(),
});

export default function EditRepositorySyncDialog({
	repository,
	asDropdownItem = false,
}: {
	repository: Repository;
	asDropdownItem?: boolean;
}) {
	const [open, setOpen] = useState(false);
	const [isLoading, setIsLoading] = useState(false);
	const [successData, setSuccessData] = useState<{
		webhookUrl?: string;
		webhookSecret?: string;
	} | null>(null);

	const form = useForm({
		defaultValues: {
			syncType: repository.syncType,
			pollingIntervalSeconds: repository.pollingIntervalSeconds ?? 300,
			githubActionsOIDCEnabled: repository.githubActionsOIDCEnabled,
		},
		validators: {
			onSubmit: syncSchema.superRefine((val, ctx) => {
				if (val.syncType === "polling") {
					if (val.pollingIntervalSeconds < 60) {
						ctx.addIssue({
							code: "custom",
							message: m.validationPollingIntervalMin(),
							path: ["pollingIntervalSeconds"],
						});
					} else if (val.pollingIntervalSeconds > 86400) {
						ctx.addIssue({
							code: "custom",
							message: m.validationPollingIntervalMax(),
							path: ["pollingIntervalSeconds"],
						});
					}
				}
			}),
		},
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				const pollingIntervalSeconds =
					value.syncType === "polling" ? value.pollingIntervalSeconds : undefined;
				const repo = await updateRepository(repository.id, {
					syncType: value.syncType,
					pollingIntervalSeconds,
					githubActionsOIDCEnabled: value.githubActionsOIDCEnabled,
				});

				setSuccessData({ webhookUrl: repo.webhookUrl, webhookSecret: repo.webhookSecret });
			} catch {
				toast.error(m.failedUpdateRepository());
			} finally {
				setIsLoading(false);
			}
		},
	});

	const handleClose = () => {
		setOpen(false);
		setSuccessData(null);
		form.reset();
	};

	return (
		<Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : handleClose())}>
			<DialogTrigger
				render={
					asDropdownItem ? (
						<DropdownMenuItem onSelect={(e) => e.preventDefault()}>
							<RefreshCwIcon />
							{m.editRepositorySyncShort()}
						</DropdownMenuItem>
					) : (
						<Button variant="ghost" size="icon">
							<RefreshCwIcon />
						</Button>
					)
				}
			></DialogTrigger>
			<DialogContent className="sm:max-w-md overflow-hidden" aria-describedby={undefined}>
				<RepositoryDialogLoadingOverlay isLoading={isLoading} />
				<DialogHeader>
					<DialogTitle>{m.editRepositorySync()}</DialogTitle>
					<DialogDescription>{m.editRepositorySyncDescription()}</DialogDescription>
				</DialogHeader>

				{successData ? (
					<div className="space-y-2 max-w-[calc(var(--container-md)-2rem)]">
						<SuccessAlert
							title={m.repositorySyncUpdatedTitle()}
							description={m.repositorySyncUpdatedDescription()}
						/>
						{successData.webhookSecret ? (
							<WebhookSetupDetails
								webhookUrl={successData.webhookUrl}
								webhookSecret={successData.webhookSecret}
							/>
						) : (
							<p className="text-sm text-muted-foreground mt-2">{m.repositoryNoFurtherAction()}</p>
						)}
						<div className="pt-2">
							<Button type="button" variant="outline" onClick={handleClose}>
								{m.close()}
							</Button>
						</div>
					</div>
				) : (
					<form
						onSubmit={async (e) => {
							e.preventDefault();
							await form.handleSubmit();
						}}
					>
						<FieldGroup>
							<form.Field name="syncType">
								{(field) => (
									<SyncTypeRadioGroup
										value={field.state.value as RepositorySyncType}
										onChange={field.handleChange}
										onBlur={field.handleBlur}
									/>
								)}
							</form.Field>

							<form.Subscribe selector={(state) => state.values.syncType}>
								{(syncType) =>
									syncType === "polling" ? (
										<form.Field
											name="pollingIntervalSeconds"
											children={(field) => {
												const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
												return (
													<Field data-invalid={isInvalid}>
														<Label htmlFor={field.name}>{m.pollingIntervalSeconds()}</Label>
														<Input
															id={field.name}
															type="number"
															min={60}
															max={86400}
															value={field.state.value ?? ""}
															onBlur={field.handleBlur}
															onChange={(e) => field.handleChange(Number(e.target.value))}
														/>
														<p className="text-muted-foreground text-xs">
															{m.pollingIntervalHint()}
														</p>
														{isInvalid && <FieldError errors={field.state.meta.errors} />}
													</Field>
												);
											}}
										/>
									) : null
								}
							</form.Subscribe>

							{repository.provider === "github" && (
								<>
									<Separator />
									<form.Field name="githubActionsOIDCEnabled">
										{(field) => (
											<div className="flex items-center justify-between gap-4">
												<div className="flex items-center gap-1.5">
													<Label htmlFor={field.name} className="cursor-pointer">
														{m.githubActionsOIDCEnabled()}
													</Label>
													<Tooltip>
														<TooltipTrigger
															render={
																<InfoIcon className="size-3.5 text-muted-foreground cursor-help" />
															}
														></TooltipTrigger>
														<TooltipContent>{m.githubActionsOIDCEnabledTooltip()}</TooltipContent>
													</Tooltip>
												</div>
												<Switch
													id={field.name}
													checked={field.state.value}
													onCheckedChange={field.handleChange}
												/>
											</div>
										)}
									</form.Field>
								</>
							)}

							<form.Subscribe selector={(state) => state.values}>
								{(values) => {
									const unchanged =
										values.syncType === repository.syncType &&
										(values.syncType !== "polling" ||
											values.pollingIntervalSeconds ===
												(repository.pollingIntervalSeconds ?? 300)) &&
										values.githubActionsOIDCEnabled === repository.githubActionsOIDCEnabled;
									return (
										<div className="flex gap-2 pt-2">
											<Button type="submit" disabled={isLoading || unchanged}>
												{m.updateRepository()}
											</Button>
											<Button
												type="button"
												variant="outline"
												onClick={handleClose}
												disabled={isLoading}
											>
												{m.cancel()}
											</Button>
										</div>
									);
								}}
							</form.Subscribe>
						</FieldGroup>
					</form>
				)}
			</DialogContent>
		</Dialog>
	);
}
