# Third-party notices

envgrid is released under the MIT licence (see `LICENSE`). The binary and the web interface include the work of others, listed here with their licences. The full licence texts ship with each package; the font licences are also in `licenses/`, because the fonts are embedded in the binary.

The licences below were read from the packages themselves: the `license` field of each npm package and the licence file of each Go module.

## Fonts (embedded in the web interface)

| Font | Licence |
| --- | --- |
| Atkinson Hyperlegible Next, from `@fontsource-variable/atkinson-hyperlegible-next` | SIL Open Font License 1.1 (`licenses/atkinson-hyperlegible-next-OFL.txt`) |
| Atkinson Hyperlegible Mono, from `@fontsource-variable/atkinson-hyperlegible-mono` | SIL Open Font License 1.1 (`licenses/atkinson-hyperlegible-mono-OFL.txt`) |

## Web interface (bundled into the binary)

| Package | Licence |
| --- | --- |
| react, react-dom | MIT |
| react-router | MIT |
| @tanstack/react-query | MIT |
| radix-ui | MIT |
| lucide-react | ISC |
| class-variance-authority | Apache-2.0 |
| clsx | MIT |
| tailwind-merge | MIT |

## Go modules (compiled into the binary)

| Module | Licence |
| --- | --- |
| github.com/go-chi/chi/v5 | MIT |
| modernc.org/sqlite, modernc.org/libc, modernc.org/mathutil, modernc.org/memory | BSD 3-Clause |
| golang.org/x/crypto, golang.org/x/sys | BSD 3-Clause |
| gopkg.in/yaml.v3 | MIT and Apache-2.0 |
| github.com/dustin/go-humanize | MIT |
| github.com/mattn/go-isatty | MIT |
| github.com/ncruces/go-strftime | MIT |
| github.com/remyoudompheng/bigfft | BSD 3-Clause |

To refresh this list after changing dependencies, run `go list -deps -f '{{if .Module}}{{.Module.Path}}{{end}}' . | sort -u` for the Go side and read the `dependencies` of `frontend/package.json` for the web side.
