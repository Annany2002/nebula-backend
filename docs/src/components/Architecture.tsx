
import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

export const Architecture: React.FC = () => {
  return (
    <section className="py-24 bg-gradient-to-b from-white to-gray-50 dark:from-gray-900 dark:to-gray-950">
      <div className="container px-4 md:px-6">
        <div className="flex flex-col items-center text-center space-y-4 mb-12">
          <div className="inline-block px-4 py-1.5 rounded-full bg-nebula-100 dark:bg-nebula-900/30">
            <p className="text-sm font-medium text-nebula-800 dark:text-nebula-300">Architecture</p>
          </div>
          <h2 className="text-3xl font-bold tracking-tighter sm:text-4xl md:text-5xl">
            Clean Project Structure
          </h2>
          <p className="mx-auto max-w-[700px] text-gray-500 md:text-xl/relaxed dark:text-gray-400">
            Designed with modern Go architecture principles
          </p>
        </div>

        <div className="grid md:grid-cols-2 gap-8">
          <div className="opacity-0 animate-fade-up">
            <Card className="h-full dark:bg-gray-800/50">
              <CardHeader>
                <CardTitle>Project Structure</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="code-block">
                  <pre className="text-sm text-gray-300">
{`nebula-backend/
├── api/                 # API layer
│   ├── handlers/        # Gin handlers
│   ├── middleware/      # Middleware
│   ├── models/          # Request/Response DTOs
│   └── router.go        # Gin router setup
├── cmd/                 # Entry point
│   └── server/          # Server application
│       └── main.go      # Main function
├── config/              # Configuration
├── internal/            # Internal logic
│   ├── auth/            # Authentication
│   ├── core/            # Core helpers
│   ├── domain/          # Domain models
│   └── storage/         # Data access
├── data/                # Runtime data
├── .env.example         # Env example
└── README.md            # Documentation`}
                  </pre>
                </div>
              </CardContent>
            </Card>
          </div>

          <div className="opacity-0 animate-fade-up delay-200">
            <Card className="h-full dark:bg-gray-800/50">
              <CardHeader>
                <CardTitle>Technology Stack</CardTitle>
              </CardHeader>
              <CardContent>
                <ul className="space-y-4">
                  <li className="flex items-start">
                    <div className="mr-2 mt-1 rounded-full bg-nebula-200 dark:bg-nebula-900 p-1">
                      <div className="h-2 w-2 rounded-full bg-nebula-600 dark:bg-nebula-400"></div>
                    </div>
                    <div>
                      <h3 className="font-medium">Go</h3>
                      <p className="text-sm text-gray-500 dark:text-gray-400">
                        Core language providing excellent performance and reliability
                      </p>
                    </div>
                  </li>
                  <li className="flex items-start">
                    <div className="mr-2 mt-1 rounded-full bg-nebula-200 dark:bg-nebula-900 p-1">
                      <div className="h-2 w-2 rounded-full bg-nebula-600 dark:bg-nebula-400"></div>
                    </div>
                    <div>
                      <h3 className="font-medium">Gin Framework</h3>
                      <p className="text-sm text-gray-500 dark:text-gray-400">
                        Web framework with excellent performance & middleware support
                      </p>
                    </div>
                  </li>
                  <li className="flex items-start">
                    <div className="mr-2 mt-1 rounded-full bg-nebula-200 dark:bg-nebula-900 p-1">
                      <div className="h-2 w-2 rounded-full bg-nebula-600 dark:bg-nebula-400"></div>
                    </div>
                    <div>
                      <h3 className="font-medium">SQLite</h3>
                      <p className="text-sm text-gray-500 dark:text-gray-400">
                        File-based database for simplified deployment in the MVP
                      </p>
                    </div>
                  </li>
                  <li className="flex items-start">
                    <div className="mr-2 mt-1 rounded-full bg-nebula-200 dark:bg-nebula-900 p-1">
                      <div className="h-2 w-2 rounded-full bg-nebula-600 dark:bg-nebula-400"></div>
                    </div>
                    <div>
                      <h3 className="font-medium">JWT Authentication</h3>
                      <p className="text-sm text-gray-500 dark:text-gray-400">
                        Secure token-based authentication system
                      </p>
                    </div>
                  </li>
                  <li className="flex items-start">
                    <div className="mr-2 mt-1 rounded-full bg-nebula-200 dark:bg-nebula-900 p-1">
                      <div className="h-2 w-2 rounded-full bg-nebula-600 dark:bg-nebula-400"></div>
                    </div>
                    <div>
                      <h3 className="font-medium">Rate Limiting</h3>
                      <p className="text-sm text-gray-500 dark:text-gray-400">
                        IP-based protection against API abuse
                      </p>
                    </div>
                  </li>
                </ul>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </section>
  );
};
