# Task repository contract fixture

`contract.json` is shared by the manager's Go domain and Web TypeScript contracts.
It covers optional aliases, default and typed refs, GitLab subgroups, two PRs in
one repository and the same external number in a different repository.

The snapshot digest is SHA-256 of `canonical_snapshot_payload` in UTF-8, with no
newline. Repository entries sort by repo ID. The digest excludes links, catalog
status and association revision; revision is provenance on the returned snapshot.
Consumers persist the selected snapshot with their preparation operation. Later
business-association edits do not change an existing operation's input.
