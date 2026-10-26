import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import {
	Bell,
	Container,
	FileText,
	GitBranch,
	LogOut,
	Menu,
	Server,
	Settings,
	User,
	XIcon,
} from "lucide-react";
import { useCallback, useState } from "react";
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

type NavKey = "applications" | "agents" | "repositories" | "notifications" | "admin";

const navItems = [
	{ key: "applications" as NavKey, href: "/applications", icon: Container },
	{ key: "agents" as NavKey, href: "/agents", icon: Server },
	{ key: "repositories" as NavKey, href: "/repositories", icon: GitBranch },
	{ key: "notifications" as NavKey, href: "/notifications", icon: Bell },
];

function getNavLabel(key: NavKey): string {
	switch (key) {
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

function NavHotkeys({ navKey }: { navKey: NavKey | "overview" }) {
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

	return (
		<header className="sticky top-0 z-50 border-b border-border bg-background/80 backdrop-blur-sm">
			<div className="flex h-14 items-center justify-between px-4">
				<div className="flex items-center gap-6">
					<Tooltip>
						<TooltipTrigger
							render={
								<Link to="/" className="flex items-center gap-3">
									<img src="/assets/logo-dark.svg" alt={m.appLogoAlt()} className="h-8 w-8" />
									<span className="font-semibold text-md hidden sm:inline">OrcaCD</span>
								</Link>
							}
						></TooltipTrigger>
						<TooltipContent>
							{m.pageOverview()}
							<NavHotkeys navKey="overview" />
						</TooltipContent>
					</Tooltip>

					<NavigationMenu className="hidden md:flex">
						<NavigationMenuList className="gap-6">
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
															className={cn(
																"relative h-14 rounded-none px-1 text-muted-foreground hover:bg-transparent hover:text-foreground focus:bg-transparent after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:rounded-full after:bg-transparent after:transition-colors",
																location.pathname.startsWith(item.href) &&
																	"text-foreground after:bg-foreground",
															)}
														>
															<item.icon className="h-4 w-4" /> {getNavLabel(item.key)}
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

				<div className="flex items-center gap-3">
					{auth.isAdmin ? (
						<Tooltip>
							<TooltipTrigger
								render={
									<Button
										variant="ghost"
										size="icon"
										aria-label={m.navAdmin()}
										className={cn(
											location.pathname.startsWith("/admin") && "bg-muted text-foreground",
										)}
										nativeButton={false}
										render={<Link to="/admin" />}
									>
										<Settings className="h-5 w-5" />
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
						className="hidden sm:flex"
						nativeButton={false}
						render={
							<a href="https://orcacd.dev" target="_blank" rel="noopener noreferrer">
								<FileText className="h-5 w-5 text-muted-foreground" />
							</a>
						}
					></Button>

					<ModeToggle
						theme={theme === "dark" ? "dark" : "light"}
						onClick={handleThemeToggle}
						variant="circle"
						start="top-right"
					/>

					<DropdownMenu>
						<DropdownMenuTrigger
							render={
								<Button variant="ghost" className="relative h-10 w-10 rounded-full">
									<Avatar className="h-10 w-10 after:pointer-events-none">
										<AvatarImage src={auth.profile?.picture} alt={auth.profile?.name} />
										<AvatarFallback>
											<Blobatar
												name={auth.profile?.id || "User"}
												animate="hover"
												className="pointer-events-auto! size-10"
											/>
										</AvatarFallback>
									</Avatar>
								</Button>
							}
						></DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-full">
							<DropdownMenuItem
								render={
									<Link to="/settings" className="flex items-center">
										<User className="mr-2 h-4 w-4" />
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
								<LogOut className="mr-2 h-4 w-4" />
								{m.signOut()}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>

					<Button
						variant="ghost"
						size="icon"
						className="md:hidden"
						onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
						aria-label={mobileMenuOpen ? m.close() : m.openMenu()}
					>
						{mobileMenuOpen ? <XIcon className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
					</Button>
				</div>
			</div>

			<div
				className={cn(
					"md:hidden grid transition-[grid-template-rows] duration-300 ease-in-out",
					mobileMenuOpen ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
				)}
			>
				<div className="overflow-hidden">
					<NavigationMenu className="border-t border-border p-4 max-w-full w-full [&>div]:w-full">
						<NavigationMenuList className="flex-col items-stretch gap-1 w-full">
							{navItems.map((item) => (
								<NavigationMenuItem key={item.key} className="w-full">
									<NavigationMenuLink
										render={
											<Link
												key={item.key}
												to={item.href}
												className={cn(
													"h-8 w-full text-muted-foreground hover:text-foreground",
													location.pathname.startsWith(item.href) && "bg-muted text-foreground",
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
