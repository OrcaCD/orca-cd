import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import {
  ChevronDown,
  FileText,
  GitBranch,
  LayoutGrid,
  LogOut,
  Menu,
  Server,
  Settings,
  User,
  X,
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
import { cn } from "@/lib/utils";
import { ModeToggle, useThemeTransition } from "./mode-toggle";
import { useTheme } from "./theme-provider";
import { useAuth } from "@/lib/auth";

const navItems = [
  { name: "Applications", href: "/applications", icon: LayoutGrid },
  { name: "Repositories", href: "/repositories", icon: GitBranch },
  { name: "Hosts", href: "/hosts", icon: Server },
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
          <Link to="/" className="flex items-center gap-2">
            <span className="font-semibold text-lg hidden sm:inline">OrcaCD</span>
          </Link>

          <nav className="hidden md:flex items-center gap-1">
            {allNavItems.map((item) => (
              <Link
                key={item.name}
                to={item.href}
                className={cn(
                  "flex items-center gap-2 px-3 py-2 text-sm rounded-md transition-colors",
                  location.pathname.startsWith(item.href)
                    ? "bg-sidebar-accent text-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-sidebar-accent",
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.name}
              </Link>
            ))}
          </nav>
        </div>

        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" className="hidden sm:flex">
            <FileText className="h-5 w-5 text-muted-foreground" />
          </Button>

          <ModeToggle
            theme={theme === "dark" ? "dark" : "light"}
            onClick={handleThemeToggle}
            variant="circle"
            start="top-right"
          />

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="flex items-center gap-2 px-2">
                <div className="h-7 w-7 rounded-full bg-primary flex items-center justify-center text-primary-foreground text-sm font-medium">
                  {auth.profile?.name?.[0]?.toUpperCase() ?? "?"}
                </div>
                <span className="hidden sm:inline text-sm">{auth.profile?.name ?? "User"}</span>
                <ChevronDown className="h-4 w-4 text-muted-foreground" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem asChild>
                <Link to="/" className="flex items-center">
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
            {mobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </Button>
        </div>
      </div>

      {mobileMenuOpen && (
        <nav className="md:hidden border-t border-border p-4 space-y-1">
          {allNavItems.map((item) => (
            <Link
              key={item.name}
              to={item.href}
              onClick={() => setMobileMenuOpen(false)}
              className={cn(
                "flex items-center gap-2 px-3 py-2 text-sm rounded-md transition-colors",
                location.pathname.startsWith(item.href)
                  ? "bg-sidebar-accent text-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-sidebar-accent",
              )}
            >
              <item.icon className="h-4 w-4" />
              {item.name}
            </Link>
          ))}
        </nav>
      )}
    </header>
  );
}
