# Features

## What it is

- A grid of keys by environment for each config file, with missing values hatched, labelled and counted.
- A compare view for any two environments: missing in A, missing in B, different, and the same, with a button to copy a value across.
- Import by uploading, dropping or pasting a file. envgrid detects the format and the keys itself: `.env`, JSON, YAML, INI, `.properties` and PHP constants (`const NAME = value;` and `define()`). Any other text file, such as notes or an nginx config, is kept whole as plain text, with the same history and compare.
- Any number of environments per repo, with names such as `staging`, `qa 2` or a branch name like `feature/login`.
- Export in the file's own format. The text formats (`.env`, JSON, YAML, INI, `.properties`) also convert into each other and into CSV; PHP and plain text files export as themselves. CSV (`key,value`) can be imported into any text file too.
- Rules per key: an admin can mark a key as required in every environment, or give it a pattern (a regular expression) that every value must match. Values that break a rule are refused when written and flagged in the grid. Keys can also carry tags, and the grid filters by tag.
- Change requests: when a member edits a value in a protected environment, the edit becomes a proposal with a reason. An admin approves it (it applies at once) or rejects it with a note on the Requests page. Approval is refused if the value changed after the request was made.
- Restore to an earlier time: put one environment of a file back to how it was at a date and time, after a preview of what would change. Each restored value is a new version, so the restore itself can be undone.
- References: a value can contain `${OTHER_KEY}`, or `${prod.OTHER_KEY}` for another environment of the same file. They are filled in when a text file is exported. Unknown names stay as written, and a cycle is an error.
- Insights: secrets that have not changed for a chosen number of days, secrets that hold the same value in more than one place, and values that break their key's rules. The page shows where, never the values.
- API tokens and a command line client: `envgrid export` prints a file for one environment and `envgrid run` starts a command with that environment's values as environment variables.
- Access that ends on a date: an admin can give a person access until the end of a chosen day.
- A choice of what the activity log records: an admin can stop recording sign-ins, value changes, structure changes or change requests. People and tokens, and deletions, are always recorded.
- Every value encrypted at rest, a full version history per value, and an audit log of who changed, revealed, copied or exported what.
- A Home page, the first screen after signing in, that says what needs attention, shows every repo against every environment, and lists the files changed most recently. The Repos page lists every repo with its coverage.
- Keyboard first: arrow keys, Enter to edit, `c` to copy, `r` to reveal a secret for ten seconds, `h` for history, `/` to filter, Ctrl+K to find any repo, file or key, `g` then a letter to move between pages, and `?` for the full list.

## What it is not

- It has no integrations. It does not sync with cloud secret managers, CI systems or Kubernetes, and nothing calls out to the internet.
- It is not a cloud service. You run it, and the data lives in one SQLite file on your disk.
- It has no single sign-on in v1. Accounts are email and password, created by an admin.
- It does not push secrets anywhere. `envgrid run` reads values when you start a command; nothing is synced or injected into running processes.

## Roadmap

- Promotion from test to uat to prod as one reviewed step, built on change requests.
- Single sign-on with OIDC.
- Scheduled rotation reminders from the Insights page.
- A Slack webhook for change requests and changes to protected environments.
