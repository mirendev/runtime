import React from 'react';
import {PageMetadata} from '@docusaurus/theme-common';
import {useDoc} from '@docusaurus/plugin-content-docs/client';
import {ogImagePath} from '../../../../plugins/og-images/path';

// Swizzled from @docusaurus/theme-classic (eject) for one change: a doc with no
// image of its own gets the card plugins/og-images renders for it at build
// time, instead of falling through to the site-wide themeConfig.image.
export default function DocItemMetadata(): React.ReactNode {
  const {metadata, frontMatter, assets} = useDoc();
  return (
    <PageMetadata
      title={metadata.title}
      description={metadata.description}
      keywords={frontMatter.keywords}
      image={assets.image ?? frontMatter.image ?? ogImagePath(metadata.permalink)}
    />
  );
}
