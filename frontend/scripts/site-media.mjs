// Finishes dist-site. It puts in the light and dark tour videos: videos are not kept in Git, so a
// local copy in ../_local/media is used when present, otherwise the asset attached to the
// "media-2" pre-release on GitHub. Without either, the site shows the poster only. It also
// replaces the app's robots.txt, which keeps self-hosted servers out of search engines, with one
// that welcomes them, plus a sitemap when SITE_URL (for example https://envgrid.dev) is set, and
// adds site/llms.txt, a plain-text map of the docs for AI tools (format: llmstxt.org).
import { copyFileSync, existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";

const out = "dist-site";
for (const theme of ["light", "dark"]) {
  const name = `envgrid-tour-${theme}.mp4`;
  copyFileSync(`../docs/tour-poster-${theme}.jpg`, `${out}/envgrid-tour-${theme}.jpg`);
  if (existsSync(`../_local/media/${name}`)) {
    copyFileSync(`../_local/media/${name}`, `${out}/${name}`);
    continue;
  }
  try {
    const res = await fetch("https://api.github.com/repos/Achal13jain/envgrid/releases/tags/media-2");
    if (!res.ok) throw new Error(`release lookup returned ${res.status}`);
    const asset = (await res.json()).assets?.find((a) => a.name === name);
    if (!asset) throw new Error(`${name} is not attached to the media-2 release`);
    const file = await fetch(asset.browser_download_url);
    if (!file.ok) throw new Error(`download returned ${file.status}`);
    writeFileSync(`${out}/${name}`, Buffer.from(await file.arrayBuffer()));
  } catch (e) {
    console.warn(`site: ${name} skipped (${e.message})`);
  }
}

const site = (process.env.SITE_URL ?? "").replace(/\/+$/, "");
let robots = "User-agent: *\nAllow: /\n";
if (site) {
  const urls = ["/", "/docs"].map((p) => `  <url><loc>${site}${p}</loc></url>`).join("\n");
  writeFileSync(
    `${out}/sitemap.xml`,
    `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`,
  );
  robots += `Sitemap: ${site}/sitemap.xml\n`;
} else {
  console.warn("site: SITE_URL is not set, so there is no sitemap");
}
writeFileSync(`${out}/robots.txt`, robots);
copyFileSync("site/llms.txt", `${out}/llms.txt`);

// One HTML file per page, each with its own title, description and canonical address, so search
// engines and link previews get them without running JavaScript. Cloudflare Pages serves docs.html
// at /docs, and with a top-level 404.html it answers unknown addresses with that page instead of
// the home page. Every page preloads the two main fonts, so text does not re-wrap when they arrive.
const shell = readFileSync(`${out}/index.html`, "utf8");
const fonts = readdirSync(`${out}/assets`).filter((f) => /-latin-wght-normal-.*\.woff2$/.test(f));
const preload = fonts.map((f) => `<link rel="preload" href="/assets/${f}" as="font" type="font/woff2" crossorigin>`);
const sameSite = "https://github.com/Achal13jain/envgrid";
const pages = [
  {
    file: "index.html",
    path: "/",
    // Proves ownership to Google Search Console. Google checks for it periodically, so it must stay.
    meta: ['<meta name="google-site-verification" content="GiMahENLw7We9Krv0UmKnXSTkJDdAIG0_7VBHaa2KH4">'],
    title: "Self-hosted env manager for teams | envgrid",
    description:
      "envgrid is an open-source, self-hosted env manager for your team's environment variables and config files: every key in every environment, side by side.",
    jsonld: {
      "@context": "https://schema.org",
      "@type": "SoftwareApplication",
      name: "envgrid",
      description:
        "Open-source, self-hosted env manager for a team's environment variables, constants and config files, shown as a grid of keys by environment.",
      applicationCategory: "DeveloperApplication",
      operatingSystem: "Linux, macOS, Windows",
      offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
      license: "https://opensource.org/licenses/MIT",
      downloadUrl: `${sameSite}/releases/latest`,
      sameAs: [sameSite],
      author: { "@type": "Person", name: "Achal Jain", url: "https://github.com/Achal13jain" },
    },
  },
  {
    file: "docs.html",
    path: "/docs",
    title: "Set up envgrid with Docker or a single binary | envgrid",
    description:
      "Install envgrid with Docker or a single binary, keep it running, let your team in, back it up and use its values in your apps. Step by step.",
  },
  { file: "404.html", title: "Page not found | envgrid", description: "This page does not exist on the envgrid website.", noindex: true },
];
for (const p of pages) {
  const head = [...preload, ...(p.meta ?? [])];
  if (p.noindex) head.push('<meta name="robots" content="noindex">');
  else if (site) head.push(`<link rel="canonical" href="${site}${p.path}">`, `<meta property="og:url" content="${site}${p.path}">`);
  if (p.jsonld) head.push(`<script type="application/ld+json">${JSON.stringify(p.jsonld).replace(/</g, "\\u003c")}</script>`);
  const html = shell
    .replace(/<title>[^<]*<\/title>/, `<title>${p.title}</title>`)
    .replace(/(<meta name="description" content=")[^"]*/, `$1${p.description}`)
    .replace(/(<meta property="og:title" content=")[^"]*/, `$1${p.title}`)
    .replace(/(<meta property="og:description" content=")[^"]*/, `$1${p.description}`)
    .replace("</head>", `  ${head.join("\n    ")}\n  </head>`);
  writeFileSync(`${out}/${p.file}`, html);
}

// Security headers for Cloudflare Pages, which reads _headers from the build output. The site
// loads only its own files plus Cloudflare Web Analytics, whose script and beacon addresses are
// from developers.cloudflare.com/web-analytics/faq/ (read 2026-10-15).
writeFileSync(
  `${out}/_headers`,
  `/*
  Content-Security-Policy: default-src 'self'; script-src 'self' https://static.cloudflareinsights.com; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; media-src 'self'; connect-src 'self' https://cloudflareinsights.com; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'
  X-Frame-Options: DENY
  Permissions-Policy: camera=(), microphone=(), geolocation=()
`,
);
