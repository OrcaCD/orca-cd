import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import {
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
import {
	NavigationMenuItem,
	NavigationMenuLink,
	NavigationMenu,
	NavigationMenuList,
} from "@/components/ui/navigation-menu";

const navItems = [
	{ name: "Applications", href: "/applications", icon: LayoutGrid },
	{ name: "Agents", href: "/agents", icon: Server },
	{ name: "Repositories", href: "/repositories", icon: GitBranch },
];

const adminNavItems = [{ name: "Admin", href: "/admin", icon: Settings }];

export default function Navbar() {
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
						<img src="/assets/logo-dark.svg" alt="OrcaCD Logo" className="h-9 w-9" />
						<span className="font-semibold text-lg hidden sm:inline">OrcaCD</span>
					</Link>

					<NavigationMenu className="hidden md:flex">
						<NavigationMenuList className="gap-2">
							{allNavItems.map((item) => (
								<NavigationMenuItem key={item.name}>
									<NavigationMenuLink asChild>
										<Link
											key={item.name}
											to={item.href}
											className={cn(
												location.pathname.startsWith(item.href) &&
													"bg-sidebar-accent text-primary-foreground hover:bg-sidebar-accent! focus:bg-sidebar-accent!",
											)}
										>
											<item.icon className="h-4 w-4" /> {item.name}
										</Link>
									</NavigationMenuLink>
								</NavigationMenuItem>
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
						<DropdownMenuContent align="end" className="w-48">
							<DropdownMenuItem asChild>
								<Link to="/settings" className="flex items-center">
									<User className="mr-2 h-4 w-4" />
									User Settings
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
								Sign Out
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>

					<Button
						variant="ghost"
						size="icon"
						className="md:hidden"
						onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
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
								<NavigationMenuItem key={item.name} className="w-full">
									<NavigationMenuLink asChild>
										<Link
											key={item.name}
											to={item.href}
											className={cn(
												"w-full",
												location.pathname.startsWith(item.href) &&
													"bg-sidebar-accent text-primary-foreground hover:bg-sidebar-accent! focus:bg-sidebar-accent!",
											)}
											onClick={() => setMobileMenuOpen(false)}
										>
											<item.icon className="h-4 w-4" /> {item.name}
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
