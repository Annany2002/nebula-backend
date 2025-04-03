import React from "react";
import { Button } from "@/components/ui/button";
import { ChevronRight, Terminal } from "lucide-react";
import { Link } from "react-router-dom";
import { ScrollArea } from "./ui/scroll-area";

export const Hero: React.FC = () => {
  return (
    <div className="relative overflow-hidden bg-gradient-to-br from-gray-900 to-nebula-950 dark:from-gray-950 dark:to-nebula-950 text-white">
      <div className="absolute inset-0 bg-grid-white/[0.03]" />
      <div className="absolute inset-0 flex items-center justify-center">
        <div className="absolute w-[600px] h-[600px] rounded-full bg-nebula-500/20 blur-3xl pointer-events-none" />
      </div>
      <div className="container relative pt-16 md:pt-24 pb-20 md:pb-32">
        <div className="flex flex-col items-center text-center space-y-8 opacity-0 animate-fade-up">
          <div className="inline-block px-4 py-2 rounded-full bg-nebula-500/10 border border-nebula-500/30 backdrop-blur-sm transition-all">
            <p className="text-sm font-medium text-nebula-300">
              Backend-as-a-Service (BaaS) MVP
            </p>
          </div>

          <h1 className="text-4xl sm:text-5xl md:text-6xl lg:text-7xl font-extrabold tracking-tight max-w-3xl">
            <span className="gradient-text">Nebula</span>
            <span className="block mt-2">Simplify Your Backend</span>
          </h1>

          <p className="max-w-[700px] text-base sm:text-lg text-gray-300">
            A powerful Backend-as-a-Service solution built with Go, enabling
            developers to focus on their front-end while Nebula handles database
            operations, authentication, and API needs.
          </p>

          <div className="flex flex-wrap gap-4 justify-center">
            <Link to="/quickstart">
              <Button
                size="lg"
                className="bg-nebula-500 hover:bg-nebula-600 text-white"
              >
                Get Started
                <ChevronRight className="ml-1 h-4 w-4" />
              </Button>
            </Link>
          </div>
        </div>

        <div className="mt-16 md:mt-24 opacity-0 animate-fade-up delay-200">
          <div className="flex justify-center">
            <div className="relative h-[300px] sm:h-[350px] md:h-[400px] w-full max-w-4xl rounded-lg border border-white/10 bg-black/30 backdrop-blur-md overflow-hidden">
              <div className="flex items-center px-4 py-2 bg-gray-900/80 border-b border-gray-700/80">
                <Terminal className="h-4 w-4 mr-2 text-gray-400" />
                <span className="text-sm font-medium text-gray-300">
                  API Examples
                </span>
              </div>
              <ScrollArea className="h-full w-full">
                <pre className="text-xs sm:text-sm text-gray-100 p-6 font-mono">
                  {`// Example: Create a new database and define a schema
TOKEN="your_jwt_token"

// Create database
curl -X POST \\
  -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"db_name": "my_app_db"}' \\
  http://localhost:8080/api/v1/databases

// Define schema
curl -X POST \\
  -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{ 
    "table_name": "tasks", 
    "columns": [ 
      {"name": "description", "type": "TEXT"}, 
      {"name": "priority", "type": "INTEGER"}, 
      {"name": "done", "type": "BOOLEAN"} 
    ] 
  }' \\
  http://localhost:8080/api/v1/databases/my_app_db/schema

// Create a record
curl -X POST \\
  -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{ "description": "Build an awesome app", "priority": 1, "done": false }' \\
  http://localhost:8080/api/v1/databases/my_app_db/tables/tasks/records`}
                </pre>
              </ScrollArea>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
