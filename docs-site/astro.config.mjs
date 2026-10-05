import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'http://localhost:4321',
  integrations: [
    starlight({
      title: 'Orion',
      description: 'Documentacao da cloud federada Orion.',
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/horizon/orion',
        },
      ],
      sidebar: [
        {
          label: 'Comece Aqui',
          items: [
            { label: 'Início', link: '/' },
            { slug: 'getting-started/overview' },
          ],
        },
        {
          label: 'Arquitetura',
          autogenerate: { directory: 'architecture' },
        },
        {
          label: 'Serviços',
          autogenerate: { directory: 'services' },
        },
        {
          label: 'Referência',
          autogenerate: { directory: 'reference' },
        },
      ],
    }),
  ],
});
