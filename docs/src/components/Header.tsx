import React from "react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { GitHubIcon } from "./icons/GitHubIcon";
import { Menu, Moon, Sun } from "lucide-react";
import { useTheme } from "@/hooks/use-theme";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import NebulaLogo from "@/assets/Nebula-Logo";

export const Header: React.FC = () => {
  const { theme, setTheme } = useTheme();
  const [isMobileMenuOpen, setIsMobileMenuOpen] = React.useState(false);

  return (
    <header className="sticky top-0 z-50 w-full border-b border-border/80 bg-background/95 backdrop-blur">
      <div className="container flex h-16 items-center justify-between">
        <div className="flex items-center">
          <Link to="/" className="flex items-center">
            <NebulaLogo />
          </Link>
        </div>

        <div className="md:hidden">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
            aria-label="Toggle menu"
          >
            <Menu className="h-5 w-5" />
          </Button>
        </div>

        <div
          className={`${
            isMobileMenuOpen
              ? "flex flex-col absolute top-16 left-0 right-0 bg-background border-b border-border p-4 gap-2 shadow-lg"
              : "hidden"
          } md:relative md:flex md:top-0 md:flex-row md:items-center md:shadow-none md:border-0 md:p-0 md:bg-transparent`}
        >
          <nav className="flex flex-col md:flex-row md:items-center md:gap-1">
            <Link to="/features" className="py-2 md:py-0">
              <Button
                variant="ghost"
                className="w-full md:w-auto text-sm font-medium"
              >
                Features
              </Button>
            </Link>
            <Link to="/api" className="py-2 md:py-0">
              <Button
                variant="ghost"
                className="w-full md:w-auto text-sm font-medium"
              >
                API
              </Button>
            </Link>
            <Link to="/architecture" className="py-2 md:py-0">
              <Button
                variant="ghost"
                className="w-full md:w-auto text-sm font-medium"
              >
                Architecture
              </Button>
            </Link>
            <Link to="/quickstart" className="py-2 md:py-0">
              <Button
                variant="ghost"
                className="w-full md:w-auto text-sm font-medium"
              >
                Quickstart
              </Button>
            </Link>
            <Link to="/roadmap" className="py-2 md:py-0">
              <Button
                variant="ghost"
                className="w-full md:w-auto text-sm font-medium"
              >
                Roadmap
              </Button>
            </Link>
          </nav>

          <div className="flex items-center gap-2 mt-4 md:mt-0 md:ml-2">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="icon" className="rounded-full">
                  {theme === "dark" ? (
                    <Moon className="h-5 w-5" />
                  ) : (
                    <Sun className="h-5 w-5" />
                  )}
                  <span className="sr-only">Toggle theme</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => setTheme("light")}>
                  <Sun className="mr-2 h-4 w-4" />
                  <span>Light</span>
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setTheme("dark")}>
                  <Moon className="mr-2 h-4 w-4" />
                  <span>Dark</span>
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setTheme("system")}>
                  <span>System</span>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            <Link
              to="https://github.com/Annany2002/nebula-backend"
              target="_blank"
              rel="noopener noreferrer"
            >
              <Button variant="outline" size="icon" className="rounded-full">
                <GitHubIcon className="h-5 w-5" />
                <span className="sr-only">GitHub</span>
              </Button>
            </Link>
          </div>
        </div>
      </div>
    </header>
  );
};
