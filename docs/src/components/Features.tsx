
import React from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Shield, Database, Table, FileText, Users, Settings, Network, Cpu, ArrowRight } from 'lucide-react';
import { Button } from './ui/button';
import { Link } from 'react-router-dom';

const features = [
  {
    icon: <Shield className="h-6 w-6 text-nebula-500" />,
    title: "Secure Authentication",
    description: "JWT-based authentication system with secure password hashing using Bcrypt."
  },
  {
    icon: <Database className="h-6 w-6 text-nebula-500" />,
    title: "Database Management",
    description: "Create, list, and delete user-owned databases with ease."
  },
  {
    icon: <Table className="h-6 w-6 text-nebula-500" />,
    title: "Schema Definition",
    description: "Define and manage table schemas with support for various data types."
  },
  {
    icon: <FileText className="h-6 w-6 text-nebula-500" />,
    title: "CRUD Operations",
    description: "Create, read, update, and delete records with basic filtering support."
  },
  {
    icon: <Network className="h-6 w-6 text-nebula-500" />,
    title: "Rate Limiting",
    description: "Basic IP-based rate limiting to protect API endpoints from abuse."
  },
  {
    icon: <Users className="h-6 w-6 text-nebula-500" />,
    title: "Multi-User Support",
    description: "Each user gets isolated database resources for complete data segregation."
  },
  {
    icon: <Settings className="h-6 w-6 text-nebula-500" />,
    title: "Configurable",
    description: "External configuration via environment variables for flexible deployment."
  },
  {
    icon: <Cpu className="h-6 w-6 text-nebula-500" />,
    title: "Go Performance",
    description: "Built with Go for excellent performance and reliability."
  }
];

export const Features: React.FC = () => {
  return (
    <section id="features" className="py-20 bg-gray-50 dark:bg-gray-900/30">
      <div className="container px-4 md:px-6">
        <div className="text-center space-y-4 mb-16">
          <h2 className="text-3xl font-bold tracking-tighter sm:text-4xl md:text-5xl">
            Key Features
          </h2>
          <p className="mx-auto max-w-[700px] text-gray-500 dark:text-gray-400 md:text-xl/relaxed">
            Everything you need to build your backend without the hassle
          </p>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-6">
          {features.map((feature, index) => (
            <div 
              key={index} 
              className="opacity-0 animate-fade-up"  
              style={{ animationDelay: `${index * 0.1}s`, animationFillMode: 'forwards' }}
            >
              <Card className="h-full transition-all hover:shadow-lg hover:-translate-y-1 dark:bg-gray-800/50 dark:border-gray-700/50">
                <CardHeader>
                  <div className="p-2 bg-nebula-100 dark:bg-nebula-900/30 rounded-full w-fit">
                    {feature.icon}
                  </div>
                  <CardTitle className="mt-4">{feature.title}</CardTitle>
                </CardHeader>
                <CardContent>
                  <CardDescription className="dark:text-gray-300">{feature.description}</CardDescription>
                </CardContent>
              </Card>
            </div>
          ))}
        </div>

        <div className="mt-16 text-center">
          <Link to="/api">
            <Button variant="outline" className="group">
              Explore API Documentation
              <ArrowRight className="ml-2 h-4 w-4 transition-transform group-hover:translate-x-1" />
            </Button>
          </Link>
        </div>
      </div>
    </section>
  );
};
