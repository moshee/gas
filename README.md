My personal homegrown web framework, take 2.

#### Features

- HTML templates
- URL routing with named capture groups (no regex)
- Rudimentary semi-ORM implementation (more on that in TODO)
  - It does not generate SQL or anything, it's only to make scanning easier and less verbose.
- User authentication (passwords stored in database using [scrypt][1])
- Persistent sessions via cookies (session IDs also stored in database (or custom store possible) using `scrypt`)
- Rudimentary signal handling

[1]: http://www.tarsnap.com/scrypt.html

#### To do

- Currently the ORM thingie only supports `SELECT` operations. Other stuff has to be done directly with `database/sql` (the package uses a single exported database instance). I'll either add the other CRUD operations or revamp the API entirely (because quite frankly it sucks).
- ~~Easier table join types by indirecting through embedded struct types used as models transparently~~ *DONE!*
- ~~There is a bunch of commented (tried but failed) code in `models.go` but I feel like I could salvage some of it.~~ *NO MORE!*
- Tests, tests, tests!
- More useful subcommands (currently only `makeuser` for creating users)
- ~~User authorization in addition to authentication~~ *YEP!*
- SSL
- ~~Markdown~~ *GOT IT!*
- More tests
  - Benchmark routers
  - Test event dispatching
- Documentation (for myself more than anyone else)
- Analytics (referer, reverse DNS, location, and the like)
- Handler func panic recovery (500 error or something) *(In progress)*
- Asset pipeline that makes dealing with/serving assets nicer, plus support for @2x images and such
