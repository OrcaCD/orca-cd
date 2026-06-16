import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import {
	Bell,
	FileText,
	GitBranch,
	LayoutGrid,
	LogOut,
	Menu,
	Server,
	Settings,
	User,
	XIcon,
} from "lucide-react";
import { useCallback, useState } from "react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn, getInitials } from "@/lib/utils";
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

type NavKey = "applications" | "agents" | "repositories" | "notifications" | "admin";

const navItems = [
	{ key: "applications" as NavKey, href: "/applications", icon: LayoutGrid },
	{ key: "agents" as NavKey, href: "/agents", icon: Server },
	{ key: "repositories" as NavKey, href: "/repositories", icon: GitBranch },
	{ key: "notifications" as NavKey, href: "/notifications", icon: Bell },
];

const adminNavItems = [{ key: "admin" as NavKey, href: "/admin", icon: Settings }];

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

	const allNavItems = auth.isAdmin ? [...navItems, ...adminNavItems] : navItems;

	return (
		<header className="sticky top-0 z-50 border-b border-border bg-background/80 backdrop-blur-sm">
			<div className="flex h-14 items-center justify-between px-4">
				<div className="flex items-center gap-6">
					<Link to="/" className="flex items-center gap-3">
						<img src="/assets/logo-dark.svg" alt={m.appLogoAlt()} className="h-9 w-9" />
						<span className="font-semibold text-lg hidden sm:inline">OrcaCD</span>
					</Link>

					<NavigationMenu className="hidden md:flex">
						<NavigationMenuList className="gap-2">
							{allNavItems.map((item) => (
								<Tooltip key={item.key}>
									<TooltipTrigger asChild>
										<NavigationMenuItem>
											<NavigationMenuLink asChild>
												<Link
													key={item.key}
													to={item.href}
													className={cn(
														location.pathname.startsWith(item.href) &&
															"bg-sidebar-accent text-primary-foreground dark:text-white hover:bg-sidebar-accent! focus:bg-sidebar-accent!",
													)}
												>
													<item.icon className="h-4 w-4" /> {getNavLabel(item.key)}
												</Link>
											</NavigationMenuLink>
										</NavigationMenuItem>
									</TooltipTrigger>
									<TooltipContent>
										{getNavLabel(item.key)}

										<KbdGroup>
											{HOTKEY_SEQUENCES[
												("navigate" +
													item.key.charAt(0).toUpperCase() +
													item.key.slice(1)) as keyof typeof HOTKEY_SEQUENCES
											].map((kbd, key) => (
												<Kbd key={key}>{kbd}</Kbd>
											))}
										</KbdGroup>
									</TooltipContent>
								</Tooltip>
							))}
						</NavigationMenuList>
					</NavigationMenu>
				</div>

				<div className="flex items-center gap-3">
					<Button variant="ghost" size="icon" className="hidden sm:flex" asChild>
						<a href="https://orcacd.dev" target="_blank" rel="noopener noreferrer">
							<FileText className="h-5 w-5 text-muted-foreground" />
						</a>
					</Button>

					<ModeToggle
						theme={theme === "dark" ? "dark" : "light"}
						onClick={handleThemeToggle}
						variant="circle"
						start="top-right"
					/>

					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="ghost" className="relative h-10 w-10 rounded-full">
								<Avatar className="h-10 w-10">
									<AvatarImage src={auth.profile?.picture || undefined} alt={auth.profile?.name} />
									<AvatarFallback>{getInitials(auth.profile?.name || "")}</AvatarFallback>
								</Avatar>
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-full">
							<DropdownMenuItem asChild>
								<Link to="/settings" className="flex items-center">
									<User className="mr-2 h-4 w-4" />
									{m.userSettings()}
								</Link>
							</DropdownMenuItem>
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
							{allNavItems.map((item) => (
								<NavigationMenuItem key={item.key} className="w-full">
									<NavigationMenuLink asChild>
										<Link
											key={item.key}
											to={item.href}
											className={cn(
												"w-full",
												location.pathname.startsWith(item.href) &&
													"bg-sidebar-accent text-primary-foreground dark:text-white hover:bg-sidebar-accent! focus:bg-sidebar-accent!",
											)}
											onClick={() => setMobileMenuOpen(false)}
										>
											<item.icon className="h-4 w-4" /> {getNavLabel(item.key)}
										</Link>
									</NavigationMenuLink>
								</NavigationMenuItem>
							))}
						</NavigationMenuList>
					</NavigationMenu>
				</div>
			</div>
		</header>
	);
}
