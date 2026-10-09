import type { ReactNode } from "react";
import { Link } from "react-router";
import { Logo, ThemeButton } from "@/components/AppShell";
import { useDocumentTitle } from "@/components/PageHeader";
import { REPO_URL } from "@/site";
import { Code } from "./Landing";

const RAW = "https://raw.githubusercontent.com/Achal13jain/envgrid/main";
const IMAGE = "ghcr.io/achal13jain/envgrid";

const C = ({ children }: { children: ReactNode }) => <code className="rounded bg-surface-2 px-1 font-mono text-[0.9em]">{children}</code>;

const settings: [string, string, string][] = [
  ["ENVGRID_MASTER_KEY", "required", "Encrypts every value. Make one with envgrid genkey and keep it in a password manager, apart from your backups. Without it the data cannot be read."],
  ["ENVGRID_ADMIN_EMAIL", "empty", "With ENVGRID_ADMIN_PASSWORD, creates the first admin on the very first start, when no user exists yet."],
  ["ENVGRID_ADMIN_PASSWORD", "empty", "The first admin's password, at least 8 characters. Change it in Settings after signing in."],
  ["ENVGRID_DATA_DIR", "/data", "The folder that holds envgrid.db. On Windows the default is %LOCALAPPDATA%\\envgrid\\data. Elsewhere, set it when you run the binary, for example ./data."],
  ["ENVGRID_LISTEN", ":8080", "Address and port to listen on. :8080 listens on every network interface; 127.0.0.1:8080 only on this machine."],
  ["ENVGRID_SECURE_COOKIES", "true", "Set to false only when people reach envgrid over plain http on a trusted network, because browsers do not send secure cookies over http."],
  ["ENVGRID_TRUST_PROXY", "false", "Set to true when envgrid runs behind a reverse proxy that adds X-Forwarded-For, so sign-in limits apply per visitor."],
  ["ENVGRID_LOG_LEVEL", "info", "debug, info, warn or error. Logs never contain values."],
];

const sections: { id: string; title: string; body: ReactNode }[] = [
  {
    id: "setup",
    title: "Pick a setup",
    body: (
      <>
        <p>
          <b>Just you.</b> Run envgrid on your own computer and open <C>http://localhost:8080</C>. Your data stays in one file on your disk.
        </p>
        <p>
          <b>A team.</b> Run envgrid on one machine everyone can reach: an office server, a cloud server or a spare computer. Everyone opens its address in a
          browser and signs in with their own account. There is nothing to install on each person&apos;s computer.
        </p>
        <p>Either way you need one secret, the master key, and an email and password for the first admin.</p>
      </>
    ),
  },
  {
    id: "docker",
    title: "Install with Docker",
    body: (
      <>
        <p>Make a master key, then start envgrid with it. The volume keeps your data between restarts and upgrades.</p>
        <Code text={`docker run --rm ${IMAGE} genkey`} />
        <Code
          text={`docker run -d --name envgrid -p 8080:8080 -v envgrid-data:/data \\
  -e ENVGRID_MASTER_KEY='paste-the-key-here' \\
  -e ENVGRID_ADMIN_EMAIL=you@example.com \\
  -e ENVGRID_ADMIN_PASSWORD='choose-a-long-password' \\
  -e ENVGRID_SECURE_COOKIES=false \\
  ${IMAGE}`}
        />
        <p>
          Open <C>http://localhost:8080</C> and sign in. To try it with demo data first, run <C>docker exec envgrid /envgrid seed</C>.
        </p>
      </>
    ),
  },
  {
    id: "binary",
    title: "Install without Docker",
    body: (
      <>
        <p>envgrid is one program with no other parts to install. The scripts below download the latest release for your system and put it on your PATH.</p>
        <h3 className="font-bold">Linux and macOS</h3>
        <Code text={`curl -fsSL ${RAW}/install.sh | sh`} />
        <Code
          text={`envgrid genkey
export ENVGRID_MASTER_KEY='paste-the-key-here'
export ENVGRID_DATA_DIR="$HOME/.envgrid"
export ENVGRID_ADMIN_EMAIL=you@example.com
export ENVGRID_ADMIN_PASSWORD='choose-a-long-password'
export ENVGRID_SECURE_COOKIES=false
envgrid serve`}
        />
        <h3 className="font-bold">Windows (PowerShell)</h3>
        <Code text={`irm ${RAW}/install.ps1 | iex`} />
        <Code
          text={`envgrid genkey
$env:ENVGRID_MASTER_KEY = 'paste-the-key-here'
$env:ENVGRID_DATA_DIR = "$env:LOCALAPPDATA\\envgrid\\data"
$env:ENVGRID_ADMIN_EMAIL = 'you@example.com'
$env:ENVGRID_ADMIN_PASSWORD = 'choose-a-long-password'
$env:ENVGRID_SECURE_COOKIES = 'false'
envgrid serve`}
        />
        <p>
          You can also download an archive for your system from the{" "}
          <a className="text-accent underline" href={`${REPO_URL}/releases/latest`}>
            latest release
          </a>{" "}
          and run the <C>envgrid</C> program inside it. Each release lists SHA-256 checksums in checksums.txt, which catch a damaged download but come from the same release as the archive.
        </p>
      </>
    ),
  },
  {
    id: "settings",
    title: "Settings",
    body: (
      <>
        <p>envgrid is configured with environment variables.</p>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-line">
                <th className="py-2 pr-4">Variable</th>
                <th className="py-2 pr-4">Default</th>
                <th className="py-2">What it does</th>
              </tr>
            </thead>
            <tbody>
              {settings.map(([name, def, text]) => (
                <tr key={name} className="border-b border-line align-top">
                  <td className="py-2 pr-4 font-mono">{name}</td>
                  <td className="py-2 pr-4 font-mono">{def}</td>
                  <td className="py-2 text-muted">{text}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </>
    ),
  },
  {
    id: "service",
    title: "Keep it running",
    body: (
      <>
        <p>
          With Docker, add <C>--restart unless-stopped</C> to the run command. On a Linux server without Docker, a systemd service starts envgrid at boot. Put
          the variables in <C>/etc/envgrid/envgrid.env</C>, readable only by root, and save this as <C>/etc/systemd/system/envgrid.service</C>:
        </p>
        <Code
          text={`[Unit]
Description=envgrid
After=network.target

[Service]
EnvironmentFile=/etc/envgrid/envgrid.env
ExecStart=/usr/local/bin/envgrid serve
Restart=on-failure
DynamicUser=yes
StateDirectory=envgrid

[Install]
WantedBy=multi-user.target`}
        />
        <p>
          Set <C>ENVGRID_DATA_DIR=/var/lib/envgrid</C> in the env file, then run <C>sudo systemctl enable --now envgrid</C>. On Windows or macOS, start{" "}
          <C>envgrid serve</C> with Task Scheduler or a login item, with the same variables set.
        </p>
      </>
    ),
  },
  {
    id: "team",
    title: "Let your team in",
    body: (
      <>
        <h3 className="font-bold">On the same network</h3>
        <p>
          Find the server&apos;s address on your network (for example 192.168.1.20) and allow port 8080 through its firewall. People then open{" "}
          <C>http://192.168.1.20:8080</C>. Plain http needs <C>ENVGRID_SECURE_COOKIES=false</C>, so use it only on a network you trust.
        </p>
        <h3 className="font-bold">Over the internet</h3>
        <p>
          Point a domain at the server and put a reverse proxy that provides HTTPS in front of envgrid, such as Caddy, nginx or Traefik. Then set{" "}
          <C>ENVGRID_LISTEN=127.0.0.1:8080</C> so only the proxy can reach envgrid, set <C>ENVGRID_TRUST_PROXY=true</C>, and leave secure cookies on.
        </p>
        <h3 className="font-bold">Add people</h3>
        <p>
          An admin opens Users, chooses Add person and picks a role. Send them the address and a temporary password; they change it in Settings. Members can
          read everything, edit unprotected environments and propose changes to protected ones such as prod. Admins approve those changes and manage people. Keep
          at least two admins, so one can reset the other&apos;s password.
        </p>
      </>
    ),
  },
  {
    id: "cli",
    title: "Use values in your apps",
    body: (
      <>
        <p>Create an API token in Settings, then use the same envgrid program as a client:</p>
        <Code
          text={`export ENVGRID_URL=https://envgrid.example.com
export ENVGRID_TOKEN=egt_...
envgrid export --repo my-repo --file backend.env --env prod > .env
envgrid run --repo my-repo --file backend.env --env prod -- node server.js`}
        />
      </>
    ),
  },
  {
    id: "backups",
    title: "Backups and upgrades",
    body: (
      <>
        <p>
          All data lives in <C>envgrid.db</C> in the data folder. To back up, stop envgrid and copy that file, or copy the whole folder. Keep the master key
          somewhere else: a backup cannot be read without it, and it cannot be recovered if lost.
        </p>
        <p>
          To upgrade, stop envgrid, replace the program (or pull the new image) and start it again. The database is updated automatically on start. Take a
          backup first.
        </p>
      </>
    ),
  },
  {
    id: "disk",
    title: "How much disk it uses",
    body: (
      <p>
        Values are text, so the database stays small. In a measured run, 50 files with 2,500 keys across 3 environments, each value changed 5 times (37,500
        stored versions with their activity entries), took 5.8 MB, about 160 bytes per stored version.
      </p>
    ),
  },
  {
    id: "help",
    title: "Troubleshooting",
    body: (
      <ul className="list-disc space-y-2 pl-5">
        <li>
          <b>Signing in does nothing over http.</b> Set <C>ENVGRID_SECURE_COOKIES=false</C>, or use HTTPS.
        </li>
        <li>
          <b>envgrid says the master key is wrong.</b> The database was created with a different key. Use the original key.
        </li>
        <li>
          <b>The port is in use.</b> Pick another with <C>ENVGRID_LISTEN=:9090</C>.
        </li>
        <li>
          <b>Someone forgot their password.</b> An admin resets it in Users.
        </li>
        <li>
          <b>Still stuck?</b>{" "}
          <a className="text-accent underline" href={`${REPO_URL}/issues`}>
            Open an issue on GitHub
          </a>
          .
        </li>
      </ul>
    ),
  },
];

/** Setup and operating guide, for one person or a whole team. */
export function DocsPage() {
  useDocumentTitle("Docs");
  return (
    <div className="min-h-dvh bg-background">
      <header className="sticky top-0 z-30 border-b border-line bg-surface/95 backdrop-blur">
        <div className="mx-auto flex h-16 max-w-6xl items-center gap-6 px-4">
          <Link to="/" className="rounded-md" aria-label="envgrid home">
            <Logo className="text-xl" />
          </Link>
          <span className="font-semibold text-muted">Docs</span>
          <div className="ml-auto flex items-center gap-2">
            <ThemeButton />
            <a href={REPO_URL} className="text-sm font-semibold hover:underline">
              GitHub
            </a>
          </div>
        </div>
      </header>
      <div className="mx-auto grid max-w-6xl gap-10 px-4 py-10 lg:grid-cols-[13rem_minmax(0,1fr)]">
        <nav aria-label="On this page" className="hidden lg:block">
          <ul className="sticky top-24 space-y-2 text-sm">
            {sections.map((s) => (
              <li key={s.id}>
                <a href={`#${s.id}`} className="text-muted hover:text-ink">
                  {s.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
        <main className="max-w-3xl space-y-12">
          <div>
            <h1 className="text-4xl font-bold tracking-tight">Set up envgrid</h1>
            <p className="mt-3 text-lg text-muted">Everything you need to run envgrid for yourself or your team, step by step. No Docker experience needed.</p>
          </div>
          {sections.map((s) => (
            <section key={s.id} id={s.id} aria-labelledby={`${s.id}-title`} className="scroll-mt-24 space-y-4 leading-relaxed">
              <h2 id={`${s.id}-title`} className="text-2xl font-bold">
                {s.title}
              </h2>
              {s.body}
            </section>
          ))}
        </main>
      </div>
    </div>
  );
}
