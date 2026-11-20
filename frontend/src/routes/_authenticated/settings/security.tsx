import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { toast } from "@/components/ui/toast";
import { useState } from "react";
import { Fingerprint, LogOut } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useAuth } from "@/lib/auth";
import { useFetch } from "@/lib/api";
import {
	addPasskey,
	deletePasskey,
	passkeyErrorMessage,
	passkeysSupported,
	type Passkey,
} from "@/lib/passkeys";
import { m } from "@/lib/paraglide/messages";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";

export const Route = createFileRoute("/_authenticated/settings/security")({
	component: SecuritySettingsPage,
	head: () => ({
		meta: [
			{
				title: m.settingsSecurityHeadTitle(),
			},
		],
	}),
});

function SecuritySettingsPage() {
	const { auth } = useAuth();
	return (
		<div className="flex flex-col gap-6">
			{auth.profile?.isLocal && !auth.profile?.demo ? (
				<PasskeysCard />
			) : (
				<Card>
					<CardHeader>
						<CardTitle>{m.passkeys()}</CardTitle>
						<CardDescription>
							{auth.profile?.demo ? m.demoBannerMessage() : m.passkeysManagedBySso()}
						</CardDescription>
					</CardHeader>
				</Card>
			)}
			<SessionsCard />
		</div>
	);
}

function PasskeysCard() {
	const { data: keys, error, mutate } = useFetch<Passkey[]>("/auth/passkeys");
	const [name, setName] = useState("");
	const [busy, setBusy] = useState(false);
	async function add(event: React.FormEvent<HTMLFormElement>) {
		event.preventDefault();
		setBusy(true);
		try {
			await addPasskey(name.trim());
			setName("");
			await mutate();
			toast.add({ type: "success", title: m.passkeyAdded() });
		} catch (err) {
			toast.add({ type: "error", title: passkeyErrorMessage(err) });
		} finally {
			setBusy(false);
		}
	}
	async function remove(id: string) {
		try {
			await deletePasskey(id);
			await mutate();
			toast.add({ type: "success", title: m.passkeyDeleted() });
		} catch (err) {
			toast.add({ type: "error", title: passkeyErrorMessage(err) });
		}
	}
	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.passkeys()}</CardTitle>
				<CardDescription>{m.passkeysDescription()}</CardDescription>
			</CardHeader>
			<CardContent className="flex flex-col gap-6">
				{error && <p role="alert">{passkeyErrorMessage(error)}</p>}
				{!keys && !error && <Skeleton className="h-16 w-full" />}
				{keys && keys.length === 0 && (
					<p className="text-sm text-muted-foreground">{m.noPasskeys()}</p>
				)}
				{keys && keys.length > 0 && (
					<ul className="flex flex-col gap-4">
						{keys.map((key) => (
							<li key={key.id} className="flex items-center justify-between gap-4">
								<div className="flex flex-col gap-1">
									<span className="font-medium">{key.name}</span>
									<span className="text-sm text-muted-foreground">
										{key.lastUsedAt
											? m.passkeyLastUsed({ date: new Date(key.lastUsedAt).toLocaleString() })
											: m.passkeyNeverUsed()}
									</span>
								</div>
								{keys.length > 1 && (
									<ConfirmationDialog
										triggerText={m.deletePasskey()}
										title={m.deletePasskey()}
										description={m.deletePasskeyDescription({ name: key.name })}
										confirmText={m.deletePasskey()}
										onConfirm={() => {
											void remove(key.id);
										}}
										triggerProps={{ variant: "outline", size: "sm" }}
									/>
								)}
							</li>
						))}
					</ul>
				)}
				{keys?.length !== 0 && (
					<>
						<form onSubmit={add} className="max-w-xl">
							<FieldGroup>
								<Field>
									<FieldLabel htmlFor="passkey-name">{m.passkeyName()}</FieldLabel>
									<Input
										id="passkey-name"
										value={name}
										onChange={(event) => setName(event.target.value)}
										maxLength={64}
										required
										placeholder={m.passkeyNamePlaceholder()}
										disabled={busy}
									/>
								</Field>
								<Field>
									<Button
										type="submit"
										disabled={busy || !passkeysSupported() || (keys?.length ?? 0) >= 20}
									>
										<Fingerprint data-icon="inline-start" />
										{busy ? m.creatingAccount() : m.addPasskey()}
									</Button>
								</Field>
							</FieldGroup>
						</form>
						{!passkeysSupported() && (
							<p role="alert" className="text-sm text-muted-foreground">
								{m.passkeysUnsupported()}
							</p>
						)}
					</>
				)}
			</CardContent>
		</Card>
	);
}

function SessionsCard() {
	const { logoutEverywhere } = useAuth();
	const navigate = useNavigate();

	async function handleLogoutEverywhere() {
		try {
			await logoutEverywhere();
			await navigate({ to: "/login" });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedSignOutEverywhere(),
			});
		}
	}

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.sessions()}</CardTitle>
				<CardDescription>{m.sessionsDescription()}</CardDescription>
			</CardHeader>
			<CardContent>
				<ConfirmationDialog
					onConfirm={handleLogoutEverywhere}
					description={m.signOutEverywhereConfirmDescription()}
					confirmText={m.signOutEverywhere()}
					triggerText={
						<>
							<LogOut className="h-4 w-4" />
							{m.signOutEverywhere()}
						</>
					}
					triggerProps={{ variant: "destructive" }}
				/>
			</CardContent>
		</Card>
	);
}
