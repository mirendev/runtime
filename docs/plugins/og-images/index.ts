import {createHash} from 'crypto';
import {copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync} from 'fs';
import {dirname, join} from 'path';
import React from 'react';
import satori from 'satori';
import {Resvg} from '@resvg/resvg-js';
import UPNG from 'upng-js';
import type {LoadContext, Plugin} from '@docusaurus/types';
import type {LoadedContent} from '@docusaurus/plugin-content-docs';
import {ogImagePath} from './path';

// Renders a 1200x630 social card for every doc into the build output. The
// swizzled DocItem/Metadata component points each page's og:image at the card
// via ogImagePath, so nothing here touches the generated HTML.
//
// Cards are static PNGs written at build time, which is all a link unfurl
// needs: Slack, Discord and the rest fetch the page and read its meta tags,
// so plain GitHub Pages hosting is enough. Satori lays out a small subset of
// CSS flexbox and resvg rasterizes the result, so no browser is involved.
//
// resvg emits unoptimized RGBA, which on a gradient comes to ~90KB a card and
// would make the cards the bulk of the site. Quantizing to a 256-color palette
// halves that with no visible difference, but the quantizer costs more than
// the render, so finished cards are kept in node_modules/.cache keyed by
// everything that feeds into them. Only pages whose title or description
// changed pay for a render, and CI restores the directory between runs.

const WIDTH = 1200;
const HEIGHT = 630;

// Brand palette from mirendev/brand: Topaz 700 -> 800 for the ground, Topaz
// 300 for secondary text on blue. The site's dark-mode logo is the white mark.
const TOPAZ_700 = '#0059FF';
const TOPAZ_800 = '#0844C5';
const TOPAZ_300 = '#80ABFF';

const fontsDir = join(__dirname, 'fonts');

function font(file: string, weight: 400 | 700) {
  return {
    name: 'Hanken Grotesk',
    data: readFileSync(join(fontsDir, file)),
    weight,
    style: 'normal' as const,
  };
}

type Card = {title: string; description: string};

// Plain createElement rather than JSX so the file loads through jiti with the
// rest of the site config. Satori only understands a flexbox subset, hence
// display: flex on every container.
const h = React.createElement;

function cardTree(card: Card, logo: string): React.ReactNode {
  const text = (style: React.CSSProperties, children: string) =>
    h('div', {style}, children);
  return h(
    'div',
    {
      style: {
        width: WIDTH,
        height: HEIGHT,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'space-between',
        padding: 72,
        background: `linear-gradient(160deg, ${TOPAZ_700} 0%, ${TOPAZ_800} 100%)`,
        color: '#FFFFFF',
        fontFamily: 'Hanken Grotesk',
      },
    },
    h('img', {src: logo, height: 56}),
    h(
      'div',
      {style: {display: 'flex', flexDirection: 'column', gap: 24}},
      // Clamped so a long title can't push the description off the bottom of
      // the card; the unfurl carries the full text anyway.
      text(
        {
          fontSize: 72,
          fontWeight: 700,
          lineHeight: 1.1,
          letterSpacing: -1.5,
          lineClamp: 2,
        },
        card.title,
      ),
      text(
        {fontSize: 32, color: TOPAZ_300, lineHeight: 1.35, lineClamp: 3},
        card.description,
      ),
    ),
    text({fontSize: 28, color: TOPAZ_300}, 'miren.md'),
  );
}

const PALETTE_COLORS = 256;

async function renderCard(
  card: Card,
  logo: string,
  fonts: ReturnType<typeof font>[],
): Promise<Buffer> {
  const svg = await satori(cardTree(card, logo), {
    width: WIDTH,
    height: HEIGHT,
    fonts,
  });
  const img = new Resvg(svg).render();
  const rgba = img.pixels.buffer.slice(
    img.pixels.byteOffset,
    img.pixels.byteOffset + img.pixels.byteLength,
  );
  return Buffer.from(
    UPNG.encode([rgba], img.width, img.height, PALETTE_COLORS),
  );
}

export default function ogImagesPlugin(context: LoadContext): Plugin<void> {
  return {
    name: 'og-images',

    async postBuild({outDir, plugins}) {
      const docsPlugins = plugins.filter(
        (p) => p.name === 'docusaurus-plugin-content-docs',
      );
      const docs = docsPlugins.flatMap((p) =>
        (p.content as LoadedContent).loadedVersions.flatMap((v) => v.docs),
      );

      const fonts = [
        font('HankenGrotesk-Regular.ttf', 400),
        font('HankenGrotesk-Bold.ttf', 700),
      ];
      const logoSvg = readFileSync(join(context.siteDir, 'static/img/logo.svg'));
      const logo = 'data:image/svg+xml;base64,' + logoSvg.toString('base64');

      // Anything that changes the pixels of every card: the template (this
      // file), the fonts and the logo. Per-card inputs are mixed in below.
      const template = createHash('sha256')
        .update(readFileSync(__filename))
        .update(logoSvg);
      for (const f of fonts) template.update(f.data);
      const templateHash = template.digest('hex');

      const cacheDir = join(context.siteDir, 'node_modules/.cache/og-images');
      mkdirSync(cacheDir, {recursive: true});

      let rendered = 0;
      for (const doc of docs) {
        const key = createHash('sha256')
          .update(templateHash)
          .update(doc.title)
          .update('\0')
          .update(doc.description)
          .digest('hex');
        const cached = join(cacheDir, `${key}.png`);
        if (!existsSync(cached)) {
          const png = await renderCard(
            {title: doc.title, description: doc.description},
            logo,
            fonts,
          );
          writeFileSync(cached, png);
          rendered++;
        }
        const out = join(outDir, ogImagePath(doc.permalink));
        mkdirSync(dirname(out), {recursive: true});
        copyFileSync(cached, out);
      }
      console.log(
        `[og-images] ${docs.length} cards, ${rendered} rendered, ${docs.length - rendered} from cache`,
      );
    },
  };
}
