import { MetadataRoute } from 'next';
import { docsNav } from './docs/content';

const BASE_URL = 'https://nester.finance';

export default function sitemap(): MetadataRoute.Sitemap {
  // Static pages
  const staticPages = [
    '',
    '/docs',
  ].map((route) => ({
    url: `${BASE_URL}${route}`,
    lastModified: new Date(),
    changeFrequency: 'weekly' as const,
    priority: route === '' ? 1 : 0.8,
  }));

  // Doc pages
  const docPages: MetadataRoute.Sitemap = [];
  
  docsNav.forEach((section) => {
    if (section.children) {
      section.children.forEach((child) => {
        docPages.push({
          url: `${BASE_URL}/docs/${child.slug}`,
          lastModified: new Date(),
          changeFrequency: 'monthly' as const,
          priority: 0.6,
        });
      });
    }
  });

  return [...staticPages, ...docPages];
}
