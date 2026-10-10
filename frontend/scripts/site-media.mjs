// Finishes dist-site. It puts in the light and dark tour videos: videos are not kept in Git, so a
// local copy in ../_local/media is used when present, otherwise the asset attached to the
// "media-2" pre-release on GitHub. Without either, the site shows the poster only. It also
// replaces the app's robots.txt, which keeps self-hosted servers out of search engines, with one
// that welcomes them, plus a sitemap when SITE_URL (for example https://envgrid.dev) is set.
import { copyFileSync, existsSync, writeFileSync } from "node:fs";

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
