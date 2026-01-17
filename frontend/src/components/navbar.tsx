import { Link, useLocation } from "@tanstack/react-router";
import { useCallback } from "react";
import { ModeToggle, useThemeTransition } from "./mode-toggle";
import { useTheme } from "./theme-provider";

import { useState } from "react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  LayoutGrid,
  GitBranch,
  Bell,
  User,
  LogOut,
  ChevronDown,
  FileText,
  HelpCircle,
  Server,
  Activity,
  Menu,
  X,
} from "lucide-react"
import { cn } from "@/lib/utils"

const navItems = [
  { name: "Applications", href: "/applications", icon: LayoutGrid },
  { name: "Repositories", href: "/repositories", icon: GitBranch },
  { name: "Hosts", href: "/hosts", icon: Server },
  { name: "Activity", href: "/activity", icon: Activity },
]

export default function Navbar() {
	const { theme, setTheme } = useTheme();
	const { startTransition } = useThemeTransition();

	const handleThemeToggle = useCallback(() => {
		const newMode = theme === "dark" ? "light" : "dark";

		startTransition(() => {
			setTheme(newMode);
		});
	}, [theme, setTheme, startTransition]);

  const location = useLocation()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)

  return (
      <header className="sticky top-0 z-50 border-b border-border bg-background/80 backdrop-blur-sm">
        <div className="flex h-14 items-center justify-between px-4">
          <div className="flex items-center gap-6">
            <Link to="/" className="flex items-center gap-2">
              <span className="font-semibold text-lg hidden sm:inline">OrcaCD</span>
            </Link>

            <nav className="hidden md:flex items-center gap-1">
              {navItems.map((item) => (
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
              <HelpCircle className="h-5 w-5 text-muted-foreground" />
            </Button>

            <Button variant="ghost" size="icon" className="hidden sm:flex">
              <FileText className="h-5 w-5 text-muted-foreground" />
            </Button>

            <Button variant="ghost" size="icon" className="relative">
              <Bell className="h-5 w-5 text-muted-foreground" />
              <span className="absolute top-1.5 right-1.5 h-2 w-2 bg-primary rounded-full" />
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
                    A
                  </div>
                  <span className="hidden sm:inline text-sm">admin</span>
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
                <DropdownMenuItem asChild>
                  <Link to="/" className="flex items-center text-destructive">
                    <LogOut className="mr-2 h-4 w-4" />
                    Sign Out
                  </Link>
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
            {navItems.map((item) => (
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
  )
}
