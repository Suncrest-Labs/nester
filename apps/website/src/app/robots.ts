import { MetadataRoute } from 'next';

export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: '*',
      allow: '/',
      disallow: ['/api/'], // Though the website doesn't have an internal /api, it points to a separate service usually
    },
    sitemap: 'https://nester.finance/sitemap.xml',
  };
}
