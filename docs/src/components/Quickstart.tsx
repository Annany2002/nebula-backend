
import React from 'react';
import { Card, CardContent } from '@/components/ui/card';
import { Check } from 'lucide-react';

export const Quickstart: React.FC = () => {
  return (
    <section id="quickstart" className="py-24 bg-gray-50 dark:bg-gray-900">
      <div className="container px-4 md:px-6">
        <div className="flex flex-col items-center text-center space-y-4 mb-12">
          <div className="inline-block px-4 py-1.5 rounded-full bg-nebula-100 dark:bg-nebula-900/30">
            <p className="text-sm font-medium text-nebula-800 dark:text-nebula-300">Quick Start</p>
          </div>
          <h2 className="text-3xl font-bold tracking-tighter sm:text-4xl md:text-5xl">
            Get Up and Running in Minutes
          </h2>
          <p className="mx-auto max-w-[700px] text-gray-500 md:text-xl/relaxed dark:text-gray-400">
            Follow these simple steps to start using Nebula
          </p>
        </div>

        <div className="grid md:grid-cols-2 gap-10">
          <div className="space-y-6 opacity-0 animate-fade-up">
            <h3 className="text-xl font-bold">Prerequisites</h3>
            <ul className="space-y-3">
              <li className="flex items-start">
                <div className="mr-2 mt-1 rounded-full bg-green-200 dark:bg-green-900/30 p-1">
                  <Check className="h-3 w-3 text-green-600 dark:text-green-400" />
                </div>
                <span>Go (version 1.18 or higher)</span>
              </li>
              <li className="flex items-start">
                <div className="mr-2 mt-1 rounded-full bg-green-200 dark:bg-green-900/30 p-1">
                  <Check className="h-3 w-3 text-green-600 dark:text-green-400" />
                </div>
                <span>Git</span>
              </li>
            </ul>
            
            <h3 className="text-xl font-bold pt-4">Installation Steps</h3>
            <Card className="dark:bg-gray-800/50">
              <CardContent className="pt-6">
                <ol className="space-y-6 pl-6 list-decimal">
                  <li>
                    <p className="font-medium">Clone the repository</p>
                    <div className="code-block mt-2">
                      <pre className="text-sm text-gray-300">
{`git clone https://github.com/Annany2002/nebula-backend.git
cd nebula-backend`}
                      </pre>
                    </div>
                  </li>
                  <li>
                    <p className="font-medium">Install Dependencies</p>
                    <div className="code-block mt-2">
                      <pre className="text-sm text-gray-300">
{`go mod tidy
# Or explicitly: go get ./...`}
                      </pre>
                    </div>
                  </li>
                  <li>
                    <p className="font-medium">Configure Environment</p>
                    <div className="code-block mt-2">
                      <pre className="text-sm text-gray-300">
{`# Copy example file
cp .env.example .env

# Edit .env file
# Set JWT_SECRET to a strong, random value
# Adjust SERVER_PORT, JWT_EXPIRATION_HOURS if needed`}
                      </pre>
                    </div>
                  </li>
                  <li>
                    <p className="font-medium">Run the Server</p>
                    <div className="code-block mt-2">
                      <pre className="text-sm text-gray-300">
{`go run ./cmd/server/main.go

# Or with air for live reloading:
air`}
                      </pre>
                    </div>
                  </li>
                </ol>
              </CardContent>
            </Card>
          </div>

          <div className="opacity-0 animate-fade-up delay-200">
            <h3 className="text-xl font-bold mb-6">What's Next?</h3>
            <div className="space-y-6">
              <div className="p-6 rounded-lg bg-white dark:bg-gray-800 shadow-sm border border-gray-200 dark:border-gray-700">
                <h4 className="text-lg font-semibold mb-2">Create Your First User</h4>
                <p className="text-gray-500 dark:text-gray-400 mb-4">Register a user to get started with the API</p>
                <div className="code-block">
                  <pre className="text-sm text-gray-300">
{`curl -X POST -H "Content-Type: application/json" \\
  -d '{"email": "your@email.com", "password": "secure-password"}' \\
  http://localhost:8080/auth/signup`}
                  </pre>
                </div>
              </div>

              <div className="p-6 rounded-lg bg-white dark:bg-gray-800 shadow-sm border border-gray-200 dark:border-gray-700">
                <h4 className="text-lg font-semibold mb-2">Login & Get Token</h4>
                <p className="text-gray-500 dark:text-gray-400 mb-4">Authenticate to receive your JWT token</p>
                <div className="code-block">
                  <pre className="text-sm text-gray-300">
{`curl -X POST -H "Content-Type: application/json" \\
  -d '{"email": "your@email.com", "password": "secure-password"}' \\
  http://localhost:8080/auth/login`}
                  </pre>
                </div>
              </div>

              <div className="p-6 rounded-lg bg-white dark:bg-gray-800 shadow-sm border border-gray-200 dark:border-gray-700">
                <h4 className="text-lg font-semibold mb-2">Create Your First Database</h4>
                <p className="text-gray-500 dark:text-gray-400 mb-4">Start building with your own database instance</p>
                <div className="code-block">
                  <pre className="text-sm text-gray-300">
{`# Replace YOUR_TOKEN with the actual JWT token
curl -X POST -H "Authorization: Bearer YOUR_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"db_name": "my_first_db"}' \\
  http://localhost:8080/api/v1/databases`}
                  </pre>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};
