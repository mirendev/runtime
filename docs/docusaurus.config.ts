import {existsSync, readFileSync} from 'fs';
import ogImagesPlugin from './plugins/og-images';
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

// The CLI loads this config through jiti when it runs under Node, which
// transpiles to CJS, where the import.meta form of the site directory is a
// syntax error. __dirname is the spelling that survives that, and cwd covers a
// loader that provides neither: every entry point runs from the site dir.
const configDir = typeof __dirname === 'string' ? __dirname : process.cwd();

// hack/docs-snapshot materializes the released docs into version-latest, which
// then serves at the site root while main serves at /next. The snapshot isn't
// checked in, so a plain checkout has no versions at all and builds main at the
// root, which is what local development wants and what PR builds get.
const releasedDocsDir = `${configDir}/versioned_docs/version-latest`;
const hasReleasedDocs = existsSync(releasedDocsDir);

// docusaurus-plugin-llms reads the docs tree itself rather than going through
// the docs plugin, so it has no idea versioning exists. Left alone it publishes
// main's markdown at the released URLs: /observability.md would serve
// unreleased docs with none of the banner the HTML page carries, and llms.txt
// would advertise pages that 404 at the root. Point it at the same snapshot the
// site root is built from. preserveDirectoryStructure: false already strips the
// configured docsDir prefix, so the emitted paths stay root-relative.
const llmsDocsDir = hasReleasedDocs ? 'versioned_docs/version-latest' : 'docs';

const releasedDocs = hasReleasedDocs
  ? {
      lastVersion: 'latest',
      // The navbar dropdown already names the version on every page, and /next
      // carries the unreleased banner on top of that. The per-page badge is a
      // third copy of the same fact sitting above the title, where the content
      // should be.
      versions: {
        latest: {
          label: readFileSync(
            `${configDir}/released-version.txt`,
            'utf8',
          ).trim(),
          path: '',
          badge: false,
        },
        current: {
          label: 'main',
          path: 'next',
          banner: 'unreleased' as const,
          badge: false,
          // Keeps /next out of search results. robots.txt can't do this job:
          // Disallow stops the crawl, which means the crawler never reads the
          // noindex, and a linked URL can still be indexed on the strength of
          // the link alone.
          noIndex: true,
        },
      },
    }
  : {};

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
    // Per-page social cards; the swizzled DocItem/Metadata component wires
    // each doc's og:image to the card this renders for it.
    ogImagesPlugin,
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
        generateMarkdownFiles: true,
        docsDir: llmsDocsDir,
        // Docs are served from the site root, so emit Markdown beside the
        // corresponding HTML route rather than under build/docs/.
        preserveDirectoryStructure: false,
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
          ...releasedDocs,
          // Every page is authored on main, including the released ones: a
          // correction is a normal docs PR, cherry-picked onto release/X.Y
          // only when it can't wait for the next release.
          editUrl: 'https://github.com/mirendev/runtime/tree/main/docs/',
        },
        blog: false,
        // /next is the same pages mid-flight. Leaving it in the sitemap would
        // offer search engines two of everything.
        sitemap: {
          ignorePatterns: ['/next/**'],
        },
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
          type: 'docsVersionDropdown',
          position: 'right',
          dropdownActiveClassDisabled: true,
        },
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
      // These are root-relative, so from /next they land on the released docs.
      // That is deliberate: the footer is site chrome pointing at the canonical
      // docs, and every /next page already carries a banner saying where the
      // reader is. Making them version-aware means shadowing Footer/LinkItem,
      // which is a theme component to maintain forever for three links.
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
      // The crawler only indexes the released docs at the site root, so there
      // is exactly one version and one locale in the index and nothing for
      // contextual filtering to narrow. Leaving it on would filter every query
      // by the page's own docusaurus_tag, which returns nothing at all from
      // /next and nothing from the root until the next crawl re-tags the
      // index. Off, /next searches the released docs, which is the best answer
      // available for pages that aren't indexed.
      contextualSearch: false,
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
