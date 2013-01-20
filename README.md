My personal homegrown web framework, take 2.

#### Features

- HTML templates
- URL routing with named capture groups (no regex)
- Rudimentary ORM implementation (more on that in TODO)
- User authentication (passwords stored in database using `scrypt`)
- Persistent sessions via cookies (session IDs also stored in database (or custom store possible) using `scrypt`)

#### To do

- Currently the ORM thingie only supports `SELECT` operations. Other stuff has to be done directly with `database/sql` (the package uses a single exported database instance). I'll either add the other CRUD operations or revamp the API entirely (because quite frankly it sucks).
- Tests, tests, tests!
- More useful subcommands (currently only `makeuser` for creating users)
- User authorization in addition to authentication
- SSL
- Markdown
- More tests
- Documentation (for myself more than anyone else)
- I'll think of more later
