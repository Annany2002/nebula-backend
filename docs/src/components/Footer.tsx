import React from "react";
import { GitHubIcon } from "./icons/GitHubIcon";
import { Link } from "react-router-dom";

export const Footer: React.FC = () => {
  return (
    <footer className="bg-gray-900 text-gray-300 py-12">
      <div className="container px-4 md:px-6">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-8">
          <div className="space-y-4">
            <div className="flex items-center">
              <div className="w-8 h-8 rounded-full bg-gradient-to-br from-nebula-400 to-nebula-700 flex items-center justify-center">
                <div className="w-5 h-5 rounded-full bg-white"></div>
              </div>
              <span className="ml-3 text-xl font-bold text-white">Nebula</span>
            </div>
            <p className="text-sm">
              A Minimum Viable Product (MVP) for a Backend-as-a-Service (BaaS)
              built using Go.
            </p>
            <div className="flex space-x-4">
              <a
                href="https://github.com/Annany2002/nebula-backend"
                target="_blank"
                rel="noopener noreferrer"
                className="hover:text-white transition-colors"
              >
                <GitHubIcon className="h-6 w-6" />
                <span className="sr-only">GitHub</span>
              </a>
            </div>
          </div>

          <div>
            <h3 className="text-white font-semibold mb-4">Documentation</h3>
            <ul className="space-y-2 text-sm">
              <li>
                <Link
                  to="/features"
                  className="hover:text-white transition-colors"
                >
                  Features
                </Link>
              </li>
              <li>
                <Link to="/api" className="hover:text-white transition-colors">
                  API Reference
                </Link>
              </li>
              <li>
                <Link
                  to="/architecture"
                  className="hover:text-white transition-colors"
                >
                  Architecture
                </Link>
              </li>
              <li>
                <Link
                  to="/quickstart"
                  className="hover:text-white transition-colors"
                >
                  Quickstart
                </Link>
              </li>
              <li>
                <Link
                  to="/roadmap"
                  className="hover:text-white transition-colors"
                >
                  Roadmap
                </Link>
              </li>
            </ul>
          </div>

          <div>
            <h3 className="text-white font-semibold mb-4">Resources</h3>
            <ul className="space-y-2 text-sm">
              <li>
                <a
                  href="https://github.com/Annany2002/nebula-backend"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:text-white transition-colors"
                >
                  GitHub Repository
                </a>
              </li>
              <li>
                <a
                  href="https://github.com/Annany2002/nebula-backend/issues"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:text-white transition-colors"
                >
                  Issue Tracker
                </a>
              </li>
              <li>
                <a
                  href="https://golang.org/doc/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:text-white transition-colors"
                >
                  Go Documentation
                </a>
              </li>
            </ul>
          </div>
        </div>

        <div className="border-t border-gray-800 mt-8 pt-8 text-sm text-center">
          <p>&copy; {new Date().getFullYear()} Nebula . All rights reserved.</p>
        </div>
      </div>
    </footer>
  );
};
