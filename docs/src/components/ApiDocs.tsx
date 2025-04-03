
import React, { useState } from 'react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Copy } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { toast } from 'sonner';

type EndpointGroup = {
  title: string;
  endpoints: Endpoint[];
};

type Endpoint = {
  method: string;
  path: string;
  description: string;
  code: string;
};

const apiGroups: EndpointGroup[] = [
  {
    title: "Authentication",
    endpoints: [
      {
        method: "POST",
        path: "/auth/signup",
        description: "Register a new user",
        code: `curl -X POST -H "Content-Type: application/json" \\
  -d '{"email": "user@example.com", "password": "password123"}' \\
  http://localhost:8080/auth/signup`
      },
      {
        method: "POST",
        path: "/auth/login",
        description: "Login and receive JWT token",
        code: `curl -X POST -H "Content-Type: application/json" \\
  -d '{"email": "user@example.com", "password": "password123"}' \\
  http://localhost:8080/auth/login`
      }
    ]
  },
  {
    title: "Database Management",
    endpoints: [
      {
        method: "POST",
        path: "/api/v1/databases",
        description: "Create a new database",
        code: `curl -X POST -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"db_name": "my_app_db"}' \\
  http://localhost:8080/api/v1/databases`
      },
      {
        method: "GET",
        path: "/api/v1/databases",
        description: "List all user databases",
        code: `curl -X GET -H "Authorization: Bearer $TOKEN" \\
  http://localhost:8080/api/v1/databases`
      },
      {
        method: "DELETE",
        path: "/api/v1/databases/{db_name}",
        description: "Delete a database",
        code: `curl -X DELETE -H "Authorization: Bearer $TOKEN" \\
  http://localhost:8080/api/v1/databases/my_app_db`
      }
    ]
  },
  {
    title: "Schema Management",
    endpoints: [
      {
        method: "POST",
        path: "/api/v1/databases/{db_name}/schema",
        description: "Create a table schema",
        code: `curl -X POST -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{ 
    "table_name": "tasks", 
    "columns": [ 
      {"name": "description", "type": "TEXT"}, 
      {"name": "priority", "type": "INTEGER"}, 
      {"name": "done", "type": "BOOLEAN"} 
    ] 
  }' \\
  http://localhost:8080/api/v1/databases/my_app_db/schema`
      },
      {
        method: "GET",
        path: "/api/v1/databases/{db_name}/tables",
        description: "List all tables in a database",
        code: `curl -X GET -H "Authorization: Bearer $TOKEN" \\
  http://localhost:8080/api/v1/databases/my_app_db/tables`
      },
      {
        method: "DELETE",
        path: "/api/v1/databases/{db_name}/tables/{table_name}",
        description: "Delete a table",
        code: `curl -X DELETE -H "Authorization: Bearer $TOKEN" \\
  http://localhost:8080/api/v1/databases/my_app_db/tables/tasks`
      }
    ]
  },
  {
    title: "Record CRUD",
    endpoints: [
      {
        method: "POST",
        path: "/api/v1/databases/{db_name}/tables/{table_name}/records",
        description: "Create a record",
        code: `curl -X POST -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{ "description": "Build an awesome app", "priority": 1, "done": false }' \\
  http://localhost:8080/api/v1/databases/my_app_db/tables/tasks/records`
      },
      {
        method: "GET",
        path: "/api/v1/databases/{db_name}/tables/{table_name}/records",
        description: "List records with optional filtering",
        code: `curl -X GET -H "Authorization: Bearer $TOKEN" \\
  "http://localhost:8080/api/v1/databases/my_app_db/tables/tasks/records?priority=1&done=false"`
      },
      {
        method: "GET",
        path: "/api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}",
        description: "Get a single record by ID",
        code: `curl -X GET -H "Authorization: Bearer $TOKEN" \\
  http://localhost:8080/api/v1/databases/my_app_db/tables/tasks/records/1`
      },
      {
        method: "PUT",
        path: "/api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}",
        description: "Update a record",
        code: `curl -X PUT -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{ "done": true }' \\
  http://localhost:8080/api/v1/databases/my_app_db/tables/tasks/records/1`
      },
      {
        method: "DELETE",
        path: "/api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}",
        description: "Delete a record",
        code: `curl -X DELETE -H "Authorization: Bearer $TOKEN" \\
  http://localhost:8080/api/v1/databases/my_app_db/tables/tasks/records/1`
      }
    ]
  }
];

export const ApiDocs: React.FC = () => {
  const [activeTab, setActiveTab] = useState("authentication");

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    toast.success('Code copied to clipboard!');
  };

  return (
    <section id="api" className="py-24">
      <div className="container px-4 md:px-6">
        <div className="flex flex-col items-center text-center space-y-4 mb-12">
          <div className="inline-block px-4 py-1.5 rounded-full bg-nebula-100 dark:bg-nebula-900/30">
            <p className="text-sm font-medium text-nebula-800 dark:text-nebula-300">API Reference</p>
          </div>
          <h2 className="text-3xl font-bold tracking-tighter sm:text-4xl md:text-5xl">
            Complete API Documentation
          </h2>
          <p className="mx-auto max-w-[700px] text-gray-500 md:text-xl/relaxed dark:text-gray-400">
            Explore the full range of Nebula API endpoints for your application
          </p>
        </div>

        <div className="opacity-0 animate-fade-up">
          <Tabs defaultValue="authentication" className="w-full">
            <TabsList className="flex overflow-auto pb-2 mb-6 gap-2 w-full">
              {apiGroups.map((group) => (
                <TabsTrigger 
                  key={group.title.toLowerCase().replace(/\s+/g, '-')}
                  value={group.title.toLowerCase().replace(/\s+/g, '-')}
                  className="whitespace-nowrap"
                  onClick={() => setActiveTab(group.title.toLowerCase().replace(/\s+/g, '-'))}
                >
                  {group.title}
                </TabsTrigger>
              ))}
            </TabsList>
            
            {apiGroups.map((group) => (
              <TabsContent 
                key={group.title.toLowerCase().replace(/\s+/g, '-')} 
                value={group.title.toLowerCase().replace(/\s+/g, '-')}
                className="space-y-6"
              >
                {group.endpoints.map((endpoint, index) => (
                  <div key={index} className="rounded-lg border bg-card overflow-hidden">
                    <div className="flex items-center justify-between bg-muted/50 px-4 py-3 border-b">
                      <div className="flex items-center gap-2">
                        <span className={`
                          px-2 py-1 text-xs font-medium rounded
                          ${endpoint.method === 'GET' ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400' : ''}
                          ${endpoint.method === 'POST' ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400' : ''}
                          ${endpoint.method === 'PUT' ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400' : ''}
                          ${endpoint.method === 'DELETE' ? 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400' : ''}
                        `}>
                          {endpoint.method}
                        </span>
                        <span className="font-mono text-sm">{endpoint.path}</span>
                      </div>
                      <span className="text-sm text-muted-foreground">{endpoint.description}</span>
                    </div>
                    <div className="relative bg-black p-4">
                      <Button 
                        variant="ghost" 
                        size="icon" 
                        className="absolute top-2 right-2 h-8 w-8 bg-gray-800 hover:bg-gray-700 text-gray-300"
                        onClick={() => copyToClipboard(endpoint.code)}
                      >
                        <Copy className="h-4 w-4" />
                        <span className="sr-only">Copy code</span>
                      </Button>
                      <pre className="text-sm text-gray-300 font-mono overflow-auto">
                        {endpoint.code}
                      </pre>
                    </div>
                  </div>
                ))}
              </TabsContent>
            ))}
          </Tabs>
        </div>
      </div>
    </section>
  );
};
