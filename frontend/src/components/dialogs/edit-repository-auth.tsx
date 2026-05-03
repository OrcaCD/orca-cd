// oxlint-disable react/no-children-prop
import { KeyRoundIcon } from "lucide-react";
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
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { updateRepository, testRepositoryConnection, type Repository } from "@/lib/repositories";
import { RepositoryDialogLoadingOverlay } from "./repository-shared";
import ErrorAlert from "@/components/alerts/error-alert";
import { m } from "@/lib/paraglide/messages";

const authSchema = z.object({
	authToken: z.string().trim().max(1024, m.validationAuthTokenMaxLength()),
});

export default function EditRepositoryAuthDialog({
	repository,
	asDropdownItem = false,
}: {
	repository: Repository;
	asDropdownItem?: boolean;
}) {
	const [open, setOpen] = useState(false);
	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<string | undefined>();

	const form = useForm({
		defaultValues: {
			authToken: "",
		},
		validators: { onSubmit: authSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				const authToken = value.authToken.trim() || undefined;
				const authMethod = authToken ? "token" : "none";
				try {
					await testRepositoryConnection({
						url: repository.url,
						provider: repository.provider,
						authMethod,
						authToken,
					});
				} catch (err: any) {
					setError(err?.message || m.failedConnectRepository());
					return;
				}
				await updateRepository(repository.id, {
					authMethod,
					authToken,
				});
				toast.success(m.repositoryAuthUpdated());
				setOpen(false);
				form.reset();
			} catch {
				toast.error(m.failedUpdateRepository());
			} finally {
				setIsLoading(false);
			}
		},
	});

	const handleClose = () => {
		setOpen(false);
		setError(undefined);
		form.reset();
	};

	return (
		<Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : handleClose())}>
			<DialogTrigger asChild>
				{asDropdownItem ? (
					<DropdownMenuItem onSelect={(e) => e.preventDefault()}>
						<KeyRoundIcon className="h-4 w-4" />
						{m.editRepositoryAuthShort()}
					</DropdownMenuItem>
				) : (
					<Button variant="ghost" size="icon">
						<KeyRoundIcon className="h-4 w-4" />
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-md overflow-hidden" aria-describedby={undefined}>
				<RepositoryDialogLoadingOverlay isLoading={isLoading} />
				<DialogHeader>
					<DialogTitle>{m.editRepositoryAuth()}</DialogTitle>
					<DialogDescription>{m.editRepositoryAuthDescription()}</DialogDescription>
				</DialogHeader>
				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<FieldGroup>
						<form.Field
							name="authToken"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>{m.authToken()}</Label>
										<Input
											id={field.name}
											type="password"
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => {
												setError(undefined);
												field.handleChange(e.target.value);
											}}
											placeholder={m.authTokenPlaceholder()}
											autoFocus
										/>
										<p className="text-muted-foreground text-xs">{m.authTokenLeaveBlankHint()}</p>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						{error && <ErrorAlert title={m.cantConnectToRepository()} description={error} />}
						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isLoading}>
								{m.updateAuthentication()}
							</Button>
							<Button type="button" variant="outline" onClick={handleClose} disabled={isLoading}>
								{m.cancel()}
							</Button>
						</div>
					</FieldGroup>
				</form>
			</DialogContent>
		</Dialog>
	);
}
