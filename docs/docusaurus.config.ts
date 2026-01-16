import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

const config: Config = {
  title: "Blob",
  tagline: "A file archive format for OCI container registries",
  favicon: "img/favicon.ico",

  future: {
    v4: true,
  },

  url: "https://blob.meigma.dev",
  baseUrl: "/",

  organizationName: "meigma",
  projectName: "blob",

  onBrokenLinks: "throw",
  onBrokenMarkdownLinks: "warn",

  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          editUrl: "https://github.com/meigma/blob/edit/master/docs/",
          routeBasePath: "/",
        },
        blog: false,
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    colorMode: {
      defaultMode: "dark",
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: "Blob",
      items: [
        {
          type: "docSidebar",
          sidebarId: "docs",
          position: "left",
          label: "Documentation",
        },
        {
          href: "https://github.com/meigma/blob",
          label: "GitHub",
          position: "right",
          className: "navbar__item--github",
        },
      ],
    },
    footer: {
      style: "dark",
      links: [
        {
          title: "Resources",
          items: [
            {
              label: "GitHub",
              href: "https://github.com/meigma/blob",
            },
            {
              label: "Go Reference",
              href: "https://pkg.go.dev/github.com/meigma/blob",
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Meigma. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ["bash", "go", "json", "yaml"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
