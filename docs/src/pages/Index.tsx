
import React from 'react';
import { Header } from '@/components/Header';
import { Hero } from '@/components/Hero';
import { Footer } from '@/components/Footer';
import { Button } from '@/components/ui/button';
import { Link } from 'react-router-dom';
import { ArrowRight } from 'lucide-react';

const Index = () => {
  const sections = [
    {
      title: "Features",
      description: "Explore the core features that Nebula offers to help you build your backend faster.",
      link: "/features"
    },
    {
      title: "API Documentation",
      description: "Detailed API endpoints for authentication, database management, and CRUD operations.",
      link: "/api"
    },
    {
      title: "Architecture",
      description: "Understand the clean project structure and technology stack behind Nebula.",
      link: "/architecture"
    },
    {
      title: "Quickstart",
      description: "Get up and running with Nebula in just a few simple steps.",
      link: "/quickstart"
    },
    {
      title: "Roadmap",
      description: "Current limitations and our future plans for Nebula's development.",
      link: "/roadmap"
    }
  ];

  return (
    <div className="flex flex-col min-h-screen">
      <Header />
      <main className="flex-1">
        <Hero />
        
        <section className="py-20 bg-gray-50 dark:bg-gray-900/30">
          <div className="container px-4 md:px-6">
            <div className="flex flex-col items-center text-center space-y-4 mb-12">
              <h2 className="text-3xl font-bold tracking-tighter sm:text-4xl md:text-5xl">
                Documentation Sections
              </h2>
              <p className="mx-auto max-w-[700px] text-gray-500 md:text-xl/relaxed dark:text-gray-400">
                Explore our documentation sections to learn all about Nebula
              </p>
            </div>
            
            <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-8">
              {sections.map((section, index) => (
                <div 
                  key={section.title}
                  className="opacity-0 animate-fade-up"
                  style={{ animationDelay: `${index * 0.1}s`, animationFillMode: 'forwards' }}
                >
                  <div className="h-full p-6 bg-white dark:bg-gray-800 dark:border-gray-700 rounded-lg border border-gray-200 shadow-sm hover:shadow-md transition-all">
                    <h3 className="text-xl font-bold mb-2">{section.title}</h3>
                    <p className="text-gray-500 dark:text-gray-400 mb-4">{section.description}</p>
                    <Link to={section.link}>
                      <Button variant="outline" className="w-full group">
                        Explore
                        <ArrowRight className="ml-2 h-4 w-4 transition-transform group-hover:translate-x-1" />
                      </Button>
                    </Link>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>
      </main>
      <Footer />
    </div>
  );
};

export default Index;
