import { useMemo, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { EllipsisVertical, Search, Send, Trash2, WebhookIcon } from "lucide-react";
import { toast } from "sonner";

import { NotificationStatusBadge } from "@/components/badges/notification-status-badge";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import CreateNotificationDialog from "@/components/dialogs/create-notification";
import { LayoutToggleGroup } from "@/components/layout-toggle-group";
import { columns } from "@/components/tables/notifications/columns";
import { NotificationsDataTable } from "@/components/tables/notifications/data-table";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { useFetch } from "@/lib/api";
import { usePreferredLayout } from "@/lib/layout-preference";
import {
	deleteNotification,
	getNotificationTypeIconPath,
	type Notification,
	testNotification,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import { toSearchableText } from "@/lib/utils";

export const Route = createFileRoute("/_authenticated/notifications/")({
	component: NotificationsPage,
	head: () => ({
		meta: [{ title: m.pageNotifications() }],
	}),
});

function NotificationsPage() {
	const { data, isLoading } = useFetch<Notification[]>("/notifications");
	const [searchQuery, setSearchQuery] = useState("");
	const { preferredLayout: viewMode, setPreferredLayout: setViewMode } = usePreferredLayout();

	async function handleDelete(item: Notification) {
		try {
			await deleteNotification(item.id);
			const identifier = item.name.trim() || item.id;
			toast.success(m.notificationDeleted({ name: identifier }));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedDeleteNotification());
		}
	}

	async function handleTest(item: Notification) {
		try {
			await testNotification(item.id);
			toast.success(m.testNotificationSent());
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedSendTestNotification());
		}
	}

	const filteredNotifications = useMemo(() => {
		const query = searchQuery.trim().toLowerCase();
		if (!query) {
			return data ?? [];
		}

		return (
			data?.filter((notification) => {
				return toSearchableText(notification).includes(query);
			}) ?? []
		);
	}, [data, searchQuery]);

	return (
		<div className="space-y-6 p-6">
			<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
				<div>
					<h1 className="text-2xl font-bold">{m.pageNotifications()}</h1>
					<p className="mt-1 text-sm text-muted-foreground">{m.notificationsPageDescription()}</p>
				</div>
				<CreateNotificationDialog />
			</div>

			{isLoading && <p className="text-sm text-muted-foreground">{m.loadingNotifications()}</p>}

			<div className="space-y-4">
				<div className="flex flex-col gap-3 pb-2 sm:flex-row sm:items-center sm:justify-between">
					<div className="relative flex-1">
						<Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
						<Input
							value={searchQuery}
							onChange={(event) => setSearchQuery(event.target.value)}
							placeholder={m.searchNotifications()}
							className="border-border bg-muted pl-9"
						/>
					</div>

					<div className="flex gap-2">
						<LayoutToggleGroup viewMode={viewMode} setViewMode={setViewMode} />
					</div>
				</div>

				{viewMode === "grid" ? (
					<>
						{filteredNotifications.length === 0 && !isLoading ? (
							<div className="rounded-xl border border-dashed p-10 text-center">
								<p className="text-sm font-medium">{m.noNotificationsFound()}</p>
								<p className="mt-1 text-sm text-muted-foreground">
									{m.noNotificationsFoundDescription()}
								</p>
							</div>
						) : null}

						<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
							{filteredNotifications.map((notification) => (
								<Card
									key={notification.id}
									className="h-full border duration-300 hover:border-primary"
								>
									<CardHeader>
										<CardAction>
											<DropdownMenu>
												<DropdownMenuTrigger
													render={
														<Button variant="ghost" size="icon" className="h-8 w-8">
															<EllipsisVertical className="h-4 w-4" />
															<span className="sr-only">{m.cardActions()}</span>
														</Button>
													}
												></DropdownMenuTrigger>
												<DropdownMenuContent align="end">
													<DropdownMenuGroup>
														<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
														<DropdownMenuItem
															onClick={() => {
																void handleTest(notification);
															}}
														>
															<Send className="h-4 w-4" />
															{m.sendTest()}
														</DropdownMenuItem>
														<DropdownMenuSeparator />
														<ConfirmationDialog
															onConfirm={() => {
																void handleDelete(notification);
															}}
															title={m.deleteNotificationTitle()}
															description={m.deleteNotificationDescription({
																name: notification.name,
															})}
															triggerText={
																<>
																	<Trash2 className="h-4 w-4" />
																	{m.delete()}
																</>
															}
															asDropdownItem
														/>
													</DropdownMenuGroup>
												</DropdownMenuContent>
											</DropdownMenu>
										</CardAction>

										<div className="flex min-w-0 items-center gap-3">
											<div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-muted/50">
												{notification.type === "webhook" ? (
													<WebhookIcon className="h-7 w-7" />
												) : (
													<img
														src={getNotificationTypeIconPath(notification.type)}
														alt={m.notificationProviderAlt()}
														className={`h-7 w-7`}
													/>
												)}
											</div>
											<div className="min-w-0 space-y-1">
												<CardTitle className="truncate" title={notification.name}>
													{notification.name}
												</CardTitle>
											</div>
										</div>
									</CardHeader>

									<hr className="mx-4" />

									<CardContent className="space-y-3">
										<NotificationStatusBadge status={notification.status} />

										<div className="grid grid-cols-1 gap-2 text-xs">
											<div className="rounded-lg border bg-muted/50 p-2">
												<p className="text-muted-foreground">{m.columnType()}</p>
												<p className="mt-1 font-medium">
													{notification.type.charAt(0).toUpperCase() + notification.type.slice(1)}
												</p>
											</div>
											<div className="rounded-lg border bg-muted/50 p-2">
												<p className="text-muted-foreground">{m.appsCount()}</p>
												<p className="mt-1 font-medium">{notification.applicationIds.length}</p>
											</div>
										</div>

										<div className="flex items-center justify-between text-sm">
											<span className="text-muted-foreground">
												{notification.enabled ? m.enabled() : m.disabled()}
											</span>
											<span className="text-muted-foreground">
												{new Date(notification.updatedAt).toLocaleString()}
											</span>
										</div>
									</CardContent>
								</Card>
							))}
						</div>
					</>
				) : (
					<NotificationsDataTable columns={columns} data={filteredNotifications} />
				)}
			</div>
		</div>
	);
}
