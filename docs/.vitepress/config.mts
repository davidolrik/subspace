import { execSync } from "node:child_process";
import { defineConfig } from "vitepress";
import { withMermaid } from "vitepress-plugin-mermaid";

const version = execSync("git describe --tags --abbrev=0 2>/dev/null || echo dev")
  .toString()
  .trim();

export default withMermaid(
  defineConfig({
    title: "Subspace",
    description: "Transparent proxy with upstream routing",
    appearance: "force-dark",

    head: [["link", { rel: "icon", type: "image/png", href: "/subspace.png" }]],

    themeConfig: {
      logo: "/subspace.png",

      search: {
        provider: "local",
      },

      nav: [
        { text: "Guide", link: "/guide/what-is-subspace" },
        { text: "Reference", link: "/reference/configuration" },
        {
          text: version,
          link: `https://github.com/davidolrik/subspace/releases/tag/${version}`,
        },
      ],

      sidebar: [
        {
          text: "Guide",
          items: [
            { text: "What is Subspace?", link: "/guide/what-is-subspace" },
            { text: "Installation", link: "/guide/installation" },
            { text: "Quick Start", link: "/guide/quick-start" },
            { text: "Configuration", link: "/guide/configuration" },
            { text: "Routing", link: "/guide/routing" },
            { text: "Commands", link: "/guide/commands" },
          ],
        },
        {
          text: "Reference",
          items: [
            { text: "Configuration", link: "/reference/configuration" },
            { text: "Pattern Matching", link: "/reference/pattern-matching" },
            {
              text: "Connection Handling",
              link: "/reference/connection-handling",
            },
            { text: "Environment", link: "/reference/environment" },
          ],
        },
      ],

      socialLinks: [
        { icon: "github", link: "https://github.com/davidolrik/subspace" },
      ],
    },
  }),
);
