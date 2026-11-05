import { Link, useLocation, useNavigate } from "@tanstack/react-router";
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

const navItems = [
	{ key: "overview" as NavKey, href: "/", icon: LayoutDashboard },
	{ key: "applications" as NavKey, href: "/applications", icon: Container },
	{ key: "agents" as NavKey, href: "/agents", icon: Server },
	{ key: "repositories" as NavKey, href: "/repositories", icon: GitBranch },
	{ key: "notifications" as NavKey, href: "/notifications", icon: Bell },
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

// The overview lives at the root, which every other path starts with, so it only matches exactly.
function isNavActive(pathname: string, href: string): boolean {
	if (href === "/") {
		return pathname === "/";
	}
	return pathname === href || pathname.startsWith(`${href}/`);
}

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
	const navigate = useNavigate();
	const { theme, setTheme } = useTheme();
	const { startTransition } = useThemeTransition();

	const handleThemeToggle = useCallback(() => {
		const newMode = theme === "dark" ? "light" : "dark";

		startTransition(() => {
			setTheme(newMode);
		});
	}, [theme, setTheme, startTransition]);

	const location = useLocation();
	const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
	const mobileMenuButtonRef = useRef<HTMLButtonElement>(null);

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
									className="flex items-center gap-2.5 rounded-md outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
								>
									<img src="/assets/logo-dark.svg" alt={m.appLogoAlt()} className="size-7" />
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
						<NavigationMenuList className="gap-0.5">
							{navItems.map((item) => (
								<Tooltip key={item.key}>
									<TooltipTrigger
										render={
											<NavigationMenuItem>
												<NavigationMenuLink
													render={
														<Link
															key={item.key}
															to={item.href}
															activeOptions={{ exact: item.href === "/" }}
															className={cn(
																"h-8 gap-2 rounded-md px-2.5 text-[0.8125rem] font-medium text-muted-foreground hover:bg-muted hover:text-foreground focus:bg-transparent xl:px-3",
																isNavActive(location.pathname, item.href) &&
																	"bg-accent text-accent-foreground hover:bg-accent focus:bg-accent [&_svg]:text-primary",
															)}
														>
															<item.icon className="size-4" />
															{/* Tablets only get the icons, the tooltip names them. */}
															<span className="max-lg:sr-only">{getNavLabel(item.key)}</span>
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
											"text-muted-foreground",
											location.pathname.startsWith("/admin") && "bg-accent text-primary",
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
									className="relative ml-1.5 size-9 rounded-full p-0 ring-1 ring-border hover:ring-primary/40"
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
					<NavigationMenu className="w-full max-w-full border-t p-3 [&>div]:w-full">
						<NavigationMenuList className="flex-col items-stretch gap-1 w-full">
							{navItems.map((item) => (
								<NavigationMenuItem key={item.key} className="w-full">
									<NavigationMenuLink
										render={
											<Link
												key={item.key}
												to={item.href}
												activeOptions={{ exact: item.href === "/" }}
												className={cn(
													"h-10 w-full px-3 font-medium text-muted-foreground hover:text-foreground",
													isNavActive(location.pathname, item.href) &&
														"bg-accent text-accent-foreground [&_svg]:text-primary",
												)}
												onClick={() => setMobileMenuOpen(false)}
											>
												<item.icon className="h-4 w-4" /> {getNavLabel(item.key)}
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
