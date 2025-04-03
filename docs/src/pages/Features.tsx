
import React from 'react';
import { Features } from '@/components/Features';
import { Header } from '@/components/Header';
import { Footer } from '@/components/Footer';

const FeaturesPage = () => {
  return (
    <div className="flex flex-col min-h-screen">
      <Header />
      <main className="flex-1 bg-background">
        <Features />
      </main>
      <Footer />
    </div>
  );
};

export default FeaturesPage;
