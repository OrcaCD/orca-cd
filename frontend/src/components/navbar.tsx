import { Link, useNavigate } from "@tanstack/react-router";
import {
	Bell,
	Container,
	FileText,
	GitBranch,
	LayoutDashboard,
	LogOut,
	Menu,
	Server,
	Settings,
	User,
	XIcon,
	type LucideIcon,
} from "lucide-react";
import { useCallback, useRef, useState } from "react";
import { Blobatar } from "@blobatar/react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { ModeToggle, useThemeTransition } from "./mode-toggle";
import { useTheme } from "./theme-provider";
import { useAuth } from "@/lib/auth";
import { usePermissions } from "@/lib/permissions";
import type { PermissionResource } from "@/lib/roles";
import { Avatar, AvatarFallback, AvatarImage } from "./ui/avatar";
import { m } from "@/lib/paraglide/messages";
import {
	NavigationMenuItem,
	NavigationMenuLink,
	NavigationMenu,
	NavigationMenuList,
} from "@/components/ui/navigation-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { Kbd, KbdGroup } from "./ui/kbd";
import { HOTKEY_SEQUENCES, useApplicationHotkeys } from "@/lib/hotkeys";
import "blobatar/motion.css";

type NavKey = "overview" | "applications" | "agents" | "repositories" | "notifications" | "admin";

const navItems: { key: NavKey; href: string; icon: LucideIcon; resource?: PermissionResource }[] = [
	{ key: "overview", href: "/", icon: LayoutDashboard },
	{ key: "applications", href: "/applications", icon: Container, resource: "application" },
	{ key: "agents", href: "/agents", icon: Server, resource: "agent" },
	{ key: "repositories", href: "/repositories", icon: GitBranch, resource: "repository" },
	{ key: "notifications", href: "/notifications", icon: Bell, resource: "notification" },
];

function getNavLabel(key: NavKey): string {
	switch (key) {
		case "overview":
			return m.pageOverview();
		case "applications":
			return m.navApplications();
		case "agents":
			return m.navAgents();
		case "repositories":
			return m.navRepositories();
		case "notifications":
			return m.navNotifications();
		case "admin":
			return m.navAdmin();
		default:
			return "";
	}
}

/*
 * The current page is marked by a primary bar on the header's bottom border instead of a filled
 * button. It is offset from the h-8 control by half the gap to the h-14 header row plus the border.
 */
const currentPageBarClassName =
	"relative after:absolute after:-bottom-[calc(0.75rem+1px)] after:h-0.5 after:scale-x-0 after:rounded-full after:bg-primary after:transition-transform after:duration-200 aria-[current=page]:after:scale-x-100";

function NavHotkeys({ navKey }: { navKey: NavKey }) {
	const sequence =
		HOTKEY_SEQUENCES[
			("navigate" +
				navKey.charAt(0).toUpperCase() +
				navKey.slice(1)) as keyof typeof HOTKEY_SEQUENCES
		];

	return (
		<KbdGroup>
			{sequence.map((kbd, key) => (
				<Kbd key={key}>{kbd}</Kbd>
			))}
		</KbdGroup>
	);
}

export default function Navbar() {
	useApplicationHotkeys();
	const { auth, logout } = useAuth();
	const { canAny } = usePermissions();
	const navigate = useNavigate();
	const { theme, setTheme } = useTheme();
	const { startTransition } = useThemeTransition();

	const handleThemeToggle = useCallback(() => {
		const newMode = theme === "dark" ? "light" : "dark";

		startTransition(() => {
			setTheme(newMode);
		});
	}, [theme, setTheme, startTransition]);

	const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
	const mobileMenuButtonRef = useRef<HTMLButtonElement>(null);
	const visibleNavItems = navItems.filter((item) => !item.resource || canAny(item.resource, "read"));
	// On desktop the logo is the link to the overview, so the bar only lists the sections.
	const sectionNavItems = visibleNavItems.filter((item) => item.key !== "overview");

	return (
		<header className="relative border-b bg-chrome backdrop-blur-xl backdrop-saturate-150">
			<div
				aria-hidden="true"
				className="waterline pointer-events-none absolute inset-x-0 -bottom-px h-px"
			/>
			<div className="flex h-14 items-center justify-between gap-4 px-4 lg:px-6">
				<div className="flex min-w-0 items-center gap-5">
					<Tooltip>
						<TooltipTrigger
							render={
								<Link
									to="/"
									activeOptions={{ exact: true }}
									aria-label={m.navHome()}
									className={cn(
										currentPageBarClassName,
										"flex h-8 items-center gap-2.5 rounded-md outline-none after:inset-x-0 after:hidden focus-visible:ring-3 focus-visible:ring-ring/50 md:after:block",
									)}
								>
									<img src="/assets/logo-dark.svg" alt="" className="size-7" />
									<span className="hidden text-[0.9375rem] font-semibold tracking-tight sm:inline">
										OrcaCD
									</span>
								</Link>
							}
						></TooltipTrigger>
						<TooltipContent>
							{m.pageOverview()}
							<NavHotkeys navKey="overview" />
						</TooltipContent>
					</Tooltip>

					<span aria-hidden="true" className="hidden h-5 w-px bg-border md:block" />

					<NavigationMenu className="hidden md:flex">
						<NavigationMenuList className="gap-1">
							{sectionNavItems.map((item) => (
								<Tooltip key={item.key}>
									<TooltipTrigger
										render={
											<NavigationMenuItem>
												<NavigationMenuLink
													render={
														<Link
															key={item.key}
															to={item.href}
															className={cn(
																currentPageBarClassName,
																"h-8 gap-2 rounded-md px-2.5 text-[0.8125rem] font-medium text-muted-foreground transition-colors after:inset-x-2.5 hover:bg-transparent hover:text-foreground focus:bg-transparent aria-[current=page]:text-foreground",
															)}
														>
															{/* Tablets drop the icons, the labels carry the navigation. */}
															<item.icon className="size-4 opacity-80 max-lg:hidden" />
															{getNavLabel(item.key)}
														</Link>
													}
												></NavigationMenuLink>
											</NavigationMenuItem>
										}
									></TooltipTrigger>
									<TooltipContent>
										{getNavLabel(item.key)}
										<NavHotkeys navKey={item.key} />
									</TooltipContent>
								</Tooltip>
							))}
						</NavigationMenuList>
					</NavigationMenu>
				</div>

				<div className="flex items-center gap-1">
					{auth.isAdmin ? (
						<Tooltip>
							<TooltipTrigger
								render={
									<Button
										variant="ghost"
										size="icon"
										aria-label={m.navAdmin()}
										className={cn(
											currentPageBarClassName,
											"text-muted-foreground after:inset-x-1.5 hover:bg-transparent aria-[current=page]:text-foreground",
										)}
										nativeButton={false}
										render={<Link to="/admin" />}
									>
										<Settings />
									</Button>
								}
							></TooltipTrigger>
							<TooltipContent>
								{m.navAdmin()}
								<NavHotkeys navKey="admin" />
							</TooltipContent>
						</Tooltip>
					) : null}

					<Button
						variant="ghost"
						size="icon"
						className="hidden text-muted-foreground sm:flex"
						aria-label={m.navDocumentation()}
						nativeButton={false}
						render={
							<a href="https://orcacd.dev" target="_blank" rel="noopener noreferrer">
								<FileText />
							</a>
						}
					></Button>

					<ModeToggle
						buttonVariant="ghost"
						className="text-muted-foreground"
						theme={theme === "dark" ? "dark" : "light"}
						onClick={handleThemeToggle}
						variant="circle"
						start="top-right"
					/>

					<DropdownMenu>
						<DropdownMenuTrigger
							render={
								<Button
									variant="ghost"
									className="relative ml-1.5 size-9 rounded-full p-0"
									aria-label={m.userSettings()}
								>
									<Avatar className="size-8 after:pointer-events-none">
										<AvatarImage src={auth.profile?.picture} alt={auth.profile?.name} />
										<AvatarFallback>
											<Blobatar
												name={auth.profile?.id || "User"}
												animate="hover"
												className="pointer-events-auto! size-8"
											/>
										</AvatarFallback>
									</Avatar>
								</Button>
							}
						></DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="min-w-56">
							{auth.profile ? (
								<>
									<div className="flex flex-col px-2 py-1.5">
										<span className="truncate text-sm font-medium">{auth.profile.name}</span>
										<span className="truncate text-xs text-muted-foreground">
											{auth.profile.email}
										</span>
									</div>
									<DropdownMenuSeparator />
								</>
							) : null}
							<DropdownMenuItem
								render={
									<Link to="/settings" className="flex items-center">
										<User className="h-4 w-4" />
										{m.userSettings()}
									</Link>
								}
							></DropdownMenuItem>
							<DropdownMenuSeparator />
							<DropdownMenuItem
								className="flex items-center text-destructive cursor-pointer"
								onClick={async () => {
									await logout();
									await navigate({ to: "/login" });
								}}
							>
								<LogOut className="h-4 w-4" />
								{m.signOut()}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>

					<Button
						ref={mobileMenuButtonRef}
						variant="ghost"
						size="icon"
						className="md:hidden"
						onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
						aria-label={mobileMenuOpen ? m.close() : m.openMenu()}
						aria-expanded={mobileMenuOpen}
						aria-controls="mobile-navigation"
					>
						{mobileMenuOpen ? <XIcon className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
					</Button>
				</div>
			</div>

			<div
				id="mobile-navigation"
				// The collapsed menu stays in the DOM for the animation, but must not be reachable by
				// keyboard or assistive technology.
				inert={!mobileMenuOpen}
				onKeyDown={(event) => {
					if (event.key === "Escape") {
						setMobileMenuOpen(false);
						mobileMenuButtonRef.current?.focus();
					}
				}}
				className={cn(
					"grid transition-[grid-template-rows] duration-300 ease-in-out motion-reduce:transition-none md:hidden",
					mobileMenuOpen ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
				)}
			>
				<div className="overflow-hidden">
					<NavigationMenu className="w-full max-w-full border-t p-4 [&>div]:w-full">
						<NavigationMenuList className="flex-col items-stretch gap-1 w-full">
							{visibleNavItems.map((item) => (
								<NavigationMenuItem key={item.key} className="w-full">
									<NavigationMenuLink
										render={
											<Link
												key={item.key}
												to={item.href}
												activeOptions={{ exact: item.href === "/" }}
												className="relative h-10 w-full gap-3 px-3 font-medium text-muted-foreground before:absolute before:inset-y-2.5 before:left-0 before:w-0.5 before:scale-y-0 before:rounded-full before:bg-primary before:transition-transform hover:bg-transparent hover:text-foreground focus:bg-transparent aria-[current=page]:text-foreground aria-[current=page]:before:scale-y-100"
												onClick={() => setMobileMenuOpen(false)}
											>
												<item.icon className="size-4 opacity-80" />
												{getNavLabel(item.key)}
											</Link>
										}
									></NavigationMenuLink>
								</NavigationMenuItem>
							))}
						</NavigationMenuList>
					</NavigationMenu>
				</div>
			</div>
		</header>
	);
}
