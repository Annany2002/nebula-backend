
import React from 'react';
import { Limitations } from '@/components/Limitations';
import { Header } from '@/components/Header';
import { Footer } from '@/components/Footer';

const RoadmapPage = () => {
  return (
    <div className="flex flex-col min-h-screen">
      <Header />
      <main className="flex-1 bg-background">
        <Limitations />
      </main>
      <Footer />
    </div>
  );
};

export default RoadmapPage;
