import { useDeferredValue, useMemo, useRef, useState } from "react";
import { PageContainer, PageHeader } from "@/components/page-header";
import {
	EmptyState,
	ResourceGridSkeleton,
	ResourceTableSkeleton,
} from "@/components/collection-states";
import { SearchField } from "@/components/search-field";
import { createFileRoute } from "@tanstack/react-router";
import { Bell, EllipsisVertical, Plus, Send, Trash2 } from "lucide-react";
import { toast } from "@/components/ui/toast";

import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import CreateNotificationDialog from "@/components/dialogs/create-notification";
import { useSharedDialog } from "@/hooks/use-shared-dialog";
import UpdateNotificationDialog from "@/components/dialogs/update-notification";
import { LayoutToggleGroup } from "@/components/layout-toggle-group";
import { columns } from "@/components/tables/notifications/columns";
import { NotificationsDataTable } from "@/components/tables/notifications/data-table";
import { Button } from "@/components/ui/button";
import { NotificationCard } from "@/components/cards/notification-card";
import {
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DialogDropdownMenu, DialogDropdownMenuContent } from "@/components/dialog-dropdown-menu";
import { useFetch } from "@/lib/api";
import { usePreferredLayout } from "@/lib/layout-preference";
import { deleteNotification, type Notification, testNotification } from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import { usePermissions } from "@/lib/permissions";
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
	const searchInputRef = useRef<HTMLInputElement>(null);
	// The list replaces the empty state as soon as a notification exists, the dialog has to stay.
	const {
		fallbackRef: addNotificationRef,
		triggerProps: addNotificationTrigger,
		openFrom: openAddNotification,
		dialogProps: addNotificationDialog,
	} = useSharedDialog();
	const { can } = usePermissions();
	const canCreate = can("notification", "create");
	const { preferredLayout: viewMode, setPreferredLayout: setViewMode } = usePreferredLayout();

	async function handleDelete(item: Notification) {
		try {
			await deleteNotification(item.id);
			const identifier = item.name.trim() || item.id;
			toast.add({ type: "success", title: m.notificationDeleted({ name: identifier }) });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeleteNotification(),
			});
		}
	}

	async function handleTest(item: Notification) {
		try {
			await testNotification(item.id);
			toast.add({ type: "success", title: m.testNotificationSent() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedSendTestNotification(),
			});
		}
	}

	const deferredSearchQuery = useDeferredValue(searchQuery);
	const filteredNotifications = useMemo(() => {
		const query = deferredSearchQuery.trim().toLowerCase();
		if (!query) {
			return data ?? [];
		}

		return (
			data?.filter((notification) => {
				return toSearchableText(notification).includes(query);
			}) ?? []
		);
	}, [data, deferredSearchQuery]);

	return (
		<PageContainer>
			<PageHeader
				title={m.pageNotifications()}
				description={m.notificationsPageDescription()}
				actions={
					canCreate && (
						<Button
							{...addNotificationTrigger}
							ref={addNotificationRef}
							onClick={openAddNotification}
						>
							<Plus />
							{m.addNotification()}
						</Button>
					)
				}
			/>
			{canCreate && <CreateNotificationDialog {...addNotificationDialog} />}

			<div className="space-y-4">
				{(data?.length !== 0 || searchQuery.length > 0) && (
					<div className="flex items-center gap-2 sm:gap-3">
						<SearchField
							value={searchQuery}
							onChange={setSearchQuery}
							inputRef={searchInputRef}
							label={m.searchNotificationsLabel()}
							placeholder={m.searchNotifications()}
							className="flex-1"
						/>

						<div className="flex shrink-0 gap-2">
							<LayoutToggleGroup viewMode={viewMode} setViewMode={setViewMode} />
						</div>
					</div>
				)}

				{isLoading ? (
					viewMode === "grid" ? (
						<ResourceGridSkeleton
							kind="notification"
							label={m.loadingNotifications()}
							className="sm:grid-cols-2 xl:grid-cols-3"
						/>
					) : (
						<ResourceTableSkeleton label={m.loadingNotifications()} />
					)
				) : data?.length === 0 ? (
					<EmptyState
						icon={Bell}
						title={m.noNotificationsYet()}
						description={m.noNotificationsYetDescription()}
						action={
							canCreate && (
								<Button {...addNotificationTrigger} onClick={openAddNotification}>
									<Plus />
									{m.addNotification()}
								</Button>
							)
						}
					/>
				) : filteredNotifications.length === 0 ? (
					<EmptyState
						variant="noMatches"
						title={m.noNotificationsFound()}
						description={m.noNotificationsFoundDescription()}
						action={
							<Button
								variant="outline"
								onClick={() => {
									setSearchQuery("");
									searchInputRef.current?.focus();
								}}
							>
								{m.searchClear()}
							</Button>
						}
					/>
				) : viewMode === "grid" ? (
					<>
						<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
							{filteredNotifications.map((notification) => {
								const canUpdate = can("notification", "update", notification.id);
								const canDelete = can("notification", "delete", notification.id);
								return (
									<NotificationCard
										key={notification.id}
										notification={notification}
										action={
											canUpdate || canDelete ? (
												<DialogDropdownMenu>
													<DropdownMenuTrigger
														render={
															<Button variant="ghost" size="icon" className="h-8 w-8">
																<EllipsisVertical className="h-4 w-4" />
																<span className="sr-only">{m.cardActions()}</span>
															</Button>
														}
													></DropdownMenuTrigger>
													<DialogDropdownMenuContent align="end">
														<DropdownMenuGroup>
															<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
															{canUpdate && (
																<>
																	<UpdateNotificationDialog
																		notification={notification}
																		asDropdownItem
																	/>
																	<DropdownMenuItem
																		onClick={() => {
																			void handleTest(notification);
																		}}
																	>
																		<Send className="h-4 w-4" />
																		{m.sendTest()}
																	</DropdownMenuItem>
																</>
															)}
															{canUpdate && canDelete && <DropdownMenuSeparator />}
															{canDelete && (
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
															)}
														</DropdownMenuGroup>
													</DialogDropdownMenuContent>
												</DialogDropdownMenu>
											) : undefined
										}
									/>
								);
							})}
						</div>
					</>
				) : (
					<NotificationsDataTable columns={columns} data={filteredNotifications} />
				)}
			</div>
		</PageContainer>
	);
}
