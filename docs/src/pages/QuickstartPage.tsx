
import React from 'react';
import { Quickstart } from '@/components/Quickstart';
import { Header } from '@/components/Header';
import { Footer } from '@/components/Footer';

const QuickstartPage = () => {
  return (
    <div className="flex flex-col min-h-screen">
      <Header />
      <main>
        <Quickstart />
      </main>
      <Footer />
    </div>
  );
};

export default QuickstartPage;
