import { defineConfig } from 'vitepress'

export default defineConfig({
  base: '/ytmdl/',
  title: 'YTMDL',
  description: 'Self-hosted music downloader, library manager, and integrated web player.',
  srcExclude: ['superpowers/**', '**/superpowers/**', 'superpowers/**/*', '**/superpowers/**/*'],
  cleanUrls: true,
  lastUpdated: true,

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/ytmdl/favicon.svg' }],
    ['meta', { name: 'theme-color', content: '#0ea5e9' }],
  ],

  themeConfig: {
    logo: '/favicon.svg',
    siteTitle: 'YTMDL',

    nav: [
      { text: 'Guide', link: '/getting-started' },
      { text: 'Deployment', link: '/deployment' },
      { text: 'Storage', link: '/storage/' },
      { text: 'API', link: '/api' },
      { text: 'Updates', link: '/updates' },
      {
        text: 'Links',
        items: [
          { text: 'GitHub Repository', link: 'https://github.com/Der-Felix/ytmdl' },
          { text: 'Legal & License', link: '/legal' },
          { text: 'Development', link: '/development' },
        ],
      },
    ],

    sidebar: [
      {
        text: 'Getting Started',
        items: [
          { text: 'Introduction', link: '/getting-started' },
          { text: 'Installation & Deployment', link: '/deployment' },
          { text: 'Configuration', link: '/configuration' },
        ],
      },
      {
        text: 'Storage & Filesystems',
        items: [
          { text: 'Storage Overview', link: '/storage/' },
          { text: 'SMB / CIFS Storage', link: '/storage/smb' },
          { text: 'NFS Storage', link: '/storage/nfs' },
          { text: 'Storage Troubleshooting', link: '/storage/troubleshooting' },
        ],
      },
      {
        text: 'Core Features',
        items: [
          { text: 'Metadata Providers', link: '/features/providers' },
          { text: 'Download Automation', link: '/features/downloads' },
          { text: 'Artist Subscriptions', link: '/features/subscriptions' },
          { text: 'Library Management & Audit', link: '/features/library' },
          { text: 'Integrated Web Player', link: '/features/player' },
          { text: 'Multi-Tier Lyrics', link: '/features/lyrics' },
        ],
      },
      {
        text: 'Administration & Operations',
        items: [
          { text: 'Users & Security', link: '/security' },
          { text: 'Update Detection & Releases', link: '/updates' },
          { text: 'Local Development', link: '/development' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'REST API Reference', link: '/api' },
          { text: 'Legal Notice & License', link: '/legal' },
        ],
      },
    ],

    search: {
      provider: 'local',
      options: {
        detailedView: true,
      },
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/Der-Felix/ytmdl' },
    ],

    footer: {
      message: 'Released under the Apache 2.0 License.',
      copyright: 'Copyright © 2026 Felix Möschen',
    },
  },
})
