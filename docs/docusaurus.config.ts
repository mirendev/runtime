import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

// Webpack 5's module resolution walks up the entire directory tree looking for
// node_modules/ directories. Ancestor directory paths that are visited during
// this walk end up in the compilation's fileDependencies and
// missingDependencies. Watchpack then creates a DirectoryWatcher on the parent
// of each watched path, which causes it to scan directories like ~/ where it
// hits Unix sockets and other special files, producing noisy ENXIO errors.
//
// This plugin intercepts the watch call to filter out any paths outside the
// project directory so watchpack stays scoped to the project tree.
// See: https://github.com/webpack/watchpack/issues/187
function filterAncestorWatchesPlugin() {
  return {
    name: 'filter-ancestor-watches',
    configureWebpack(config) {
      config.plugins.push({
        apply(compiler) {
          const siteDir = compiler.context;
          const origWatch = compiler.watchFileSystem.watch.bind(
            compiler.watchFileSystem,
          );
          compiler.watchFileSystem.watch = (
            files,
            dirs,
            missing,
            ...rest
          ) => {
            const filteredFiles = [...files].filter((f) =>
              f.startsWith(siteDir),
            );
            const filteredMissing = [...missing].filter((m) =>
              m.startsWith(siteDir),
            );
            return origWatch(filteredFiles, dirs, filteredMissing, ...rest);
          };
        },
      });
      return {};
    },
  };
}

const config: Config = {
  title: 'Miren Docs',
  tagline: 'Enjoy the Deploy',
  favicon: 'img/favicon.png',

  // Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
  future: {
    v4: true, // Improve compatibility with the upcoming Docusaurus v4
  },

  // Set the production url of your site here
  url: 'https://miren.md',
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment, it is often '/<projectName>/'
  baseUrl: '/',

  // GitHub pages deployment config.
  // If you aren't using GitHub pages, you don't need these.
  organizationName: 'mirendev',
  projectName: 'runtime',

  // Prevent GitHub Pages from adding trailing slashes via redirects
  trailingSlash: false,

  // Render ```mermaid fenced blocks as diagrams (e.g. the Miren Anywhere
  // request-flow diagram). Requires @docusaurus/theme-mermaid, registered below.
  markdown: {
    mermaid: true,
  },
  themes: ['@docusaurus/theme-mermaid'],

  plugins: [
    filterAncestorWatchesPlugin,
    [
      '@docusaurus/plugin-client-redirects',
      {
        redirects: [
          // /languages was merged into the Language Guides index. Keep the old
          // URL alive for external links and search results.
          {from: '/languages', to: '/guides'},
        ],
      },
    ],
    [
      'docusaurus-plugin-llms',
      {
        generateLLMsTxt: true,
        generateLLMsFullTxt: true,
        excludeImports: true,
        removeDuplicateHeadings: true,
        logLevel: 'quiet',
      },
    ],
  ],

  scripts: [
    {
      src: 'https://cdn.usefathom.com/script.js',
      'data-site': 'MEHJPZGQ',
      'data-spa': 'auto',
      defer: true,
    },
  ],

  onBrokenLinks: 'throw',

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang. For example, if your site is Chinese, you
  // may want to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/mirendev/runtime/tree/main/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    // The theme always emits twitter:card=summary_large_image, so without an
    // image every shared link renders as a broken large-image card. Shared with
    // the marketing site (mirendev/public/miren-og-card.png); keep them in sync.
    image: 'img/miren-og-card.png',
    metadata: [
      {property: 'og:type', content: 'website'},
      {property: 'og:site_name', content: 'Miren Docs'},
    ],
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: '',
      logo: {
        alt: 'Miren Docs',
        src: 'img/logo-light.svg',
        srcDark: 'img/logo.svg',
      },
      items: [
{
          type: 'search',
          position: 'right',
        },
        {
          href: 'https://github.com/mirendev/runtime',
          label: 'GitHub',
          position: 'right',
          'aria-label': 'GitHub repository (opens in new tab)',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {
              label: 'Getting Started',
              to: '/',
            },
            {
              label: 'CLI Reference',
              to: '/commands',
            },
          ],
        },
        {
          title: 'Community',
          items: [
            {
              label: 'Code of Conduct',
              to: '/conduct',
            },
            {
              label: 'GitHub',
              href: 'https://github.com/mirendev/runtime',
            },
          ],
        },
      ],
      copyright: `© ${new Date().getFullYear()} From your friends at <a href="https://miren.dev" target="_blank" rel="noopener noreferrer">Miren</a>`,
    },
    algolia: {
      appId: 'UMQ0GOVXIG',
      apiKey: '9ac12cfcf7f3cdb2ccd3fe48548cc1ed',
      indexName: 'Miren Docs',
      contextualSearch: true,
      searchPagePath: 'search',
      // Send click & conversion events to Algolia so we can see which search
      // results people actually open (Click Analytics in the Algolia
      // dashboard). DocSearch handles the queryID/userToken plumbing.
      insights: true,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'toml'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
