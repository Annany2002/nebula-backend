
import React from 'react';
import { Architecture } from '@/components/Architecture';
import { Header } from '@/components/Header';
import { Footer } from '@/components/Footer';

const ArchitecturePage = () => {
  return (
    <div className="flex flex-col min-h-screen">
      <Header />
      <main>
        <Architecture />
      </main>
      <Footer />
    </div>
  );
};

export default ArchitecturePage;
