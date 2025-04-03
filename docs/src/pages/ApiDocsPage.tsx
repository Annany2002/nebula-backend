
import React from 'react';
import { ApiDocs } from '@/components/ApiDocs';
import { Header } from '@/components/Header';
import { Footer } from '@/components/Footer';

const ApiDocsPage = () => {
  return (
    <div className="flex flex-col min-h-screen">
      <Header />
      <main className="flex-1 bg-background">
        <ApiDocs />
      </main>
      <Footer />
    </div>
  );
};

export default ApiDocsPage;
