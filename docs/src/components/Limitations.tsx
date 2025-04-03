
import React from 'react';
import { ArrowRight } from 'lucide-react';

export const Limitations: React.FC = () => {
  return (
    <section className="py-24">
      <div className="container px-4 md:px-6">
        <div className="flex flex-col items-center text-center space-y-4 mb-12">
          <div className="inline-block px-4 py-1.5 rounded-full bg-nebula-100 dark:bg-nebula-900/30">
            <p className="text-sm font-medium text-nebula-800 dark:text-nebula-300">Roadmap</p>
          </div>
          <h2 className="text-3xl font-bold tracking-tighter sm:text-4xl md:text-5xl">
            Current Limitations & Future Work
          </h2>
          <p className="mx-auto max-w-[700px] text-gray-500 md:text-xl/relaxed dark:text-gray-400">
            Nebula is an MVP with plans for significant enhancements
          </p>
        </div>

        <div className="grid md:grid-cols-2 gap-10 opacity-0 animate-fade-up">
          <div>
            <h3 className="text-xl font-bold mb-6">Current Limitations</h3>
            <ul className="space-y-4">
              <li className="flex items-start">
                <div className="mr-3 mt-1 rounded-full bg-red-100 dark:bg-red-900/30 p-1">
                  <div className="h-2 w-2 rounded-full bg-red-600 dark:bg-red-400"></div>
                </div>
                <div>
                  <p className="font-medium">Scalability Constraints</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    The SQLite file-per-user approach is not suitable for high-scale production use.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 mt-1 rounded-full bg-red-100 dark:bg-red-900/30 p-1">
                  <div className="h-2 w-2 rounded-full bg-red-600 dark:bg-red-400"></div>
                </div>
                <div>
                  <p className="font-medium">Limited Querying Capabilities</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Only basic equality filtering is available without pagination or sorting.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 mt-1 rounded-full bg-red-100 dark:bg-red-900/30 p-1">
                  <div className="h-2 w-2 rounded-full bg-red-600 dark:bg-red-400"></div>
                </div>
                <div>
                  <p className="font-medium">No Schema Migrations</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    No system for evolving metadata.db schema or user schemas.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 mt-1 rounded-full bg-red-100 dark:bg-red-900/30 p-1">
                  <div className="h-2 w-2 rounded-full bg-red-600 dark:bg-red-400"></div>
                </div>
                <div>
                  <p className="font-medium">Basic Security</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    While JWT and rate limiting are implemented, deeper input sanitization and RBAC are needed.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 mt-1 rounded-full bg-red-100 dark:bg-red-900/30 p-1">
                  <div className="h-2 w-2 rounded-full bg-red-600 dark:bg-red-400"></div>
                </div>
                <div>
                  <p className="font-medium">Documentation</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    No Swagger/OpenAPI specification for API documentation.
                  </p>
                </div>
              </li>
            </ul>
          </div>

          <div>
            <h3 className="text-xl font-bold mb-6">Future Roadmap</h3>
            <ul className="space-y-4">
              <li className="flex items-start">
                <div className="mr-3 rounded-full bg-blue-100 dark:bg-blue-900/30 p-1">
                  <ArrowRight className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                </div>
                <div>
                  <p className="font-medium">Database Migration</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Transition from SQLite to PostgreSQL/MySQL for improved scalability and concurrent access.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 rounded-full bg-blue-100 dark:bg-blue-900/30 p-1">
                  <ArrowRight className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                </div>
                <div>
                  <p className="font-medium">Advanced Querying</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Add pagination, sorting, and advanced filtering operators for more powerful data retrieval.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 rounded-full bg-blue-100 dark:bg-blue-900/30 p-1">
                  <ArrowRight className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                </div>
                <div>
                  <p className="font-medium">Schema Migrations</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Implement a migration tool for evolving database schemas over time.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 rounded-full bg-blue-100 dark:bg-blue-900/30 p-1">
                  <ArrowRight className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                </div>
                <div>
                  <p className="font-medium">Enhanced Security</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Add RBAC, resource usage limits, and deeper input validation.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 rounded-full bg-blue-100 dark:bg-blue-900/30 p-1">
                  <ArrowRight className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                </div>
                <div>
                  <p className="font-medium">Additional BaaS Features</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Add file storage, serverless functions, and real-time features.
                  </p>
                </div>
              </li>

              <li className="flex items-start">
                <div className="mr-3 rounded-full bg-blue-100 dark:bg-blue-900/30 p-1">
                  <ArrowRight className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                </div>
                <div>
                  <p className="font-medium">Comprehensive Testing</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    Expand test coverage with integration tests for all endpoints.
                  </p>
                </div>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
};
