import React from 'react';
import Head from 'next/head';
import Link from 'next/link';

export default function Home() {
  return (
    <>
      <Head>
        <title>Nester — DeFi Yield Optimizer</title>
        <meta
          name="description"
          content="Nester is a decentralized yield optimizer on Stellar."
        />
      </Head>
      <main className="min-h-screen bg-gradient-to-br from-indigo-500 to-purple-600 flex flex-col items-center justify-center text-white px-4">
        <h1 className="text-5xl font-bold mb-4">Nester</h1>
        <p className="text-xl mb-8">
          Decentralized yield optimization on Stellar
        </p>
        <div className="flex space-x-4">
          <Link
            href="/app"
            className="bg-white text-indigo-600 px-6 py-3 rounded-lg font-semibold hover:bg-gray-100 transition"
          >
            Launch App
          </Link>
          <Link
            href="/security"
            className="bg-transparent border-2 border-white px-6 py-3 rounded-lg font-semibold hover:bg-white hover:text-indigo-600 transition"
          >
            Security
          </Link>
        </div>
      </main>
    </>
  );
}
