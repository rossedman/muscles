# Test data

`rpmdb.sqlite` is a real SQLite database in rpm's layout (a `Packages`
table of `hnum`, `blob`), written by Python's `sqlite3` with 512-byte pages
so the table has interior pages and its largest header spills onto overflow
pages. It holds `bash 5.2.32-1.fc41`, `openssl-libs 1:3.2.6-2.fc41`, a
`gpg-pubkey` key entry (skipped), `pkg00` to `pkg39` and a 3 KB
`kernel-core 6.11.4-301.fc41` header.

The Berkeley DB and ndb files are built in `rpm_test.go`. The parsers were
also checked against the rpm databases of fedora:41, ubi9/ubi-minimal,
rockylinux:9, amazonlinux:2023 (SQLite), ubi8/ubi-minimal, amazonlinux:2
(Berkeley DB) and opensuse/leap:15.6 (ndb); those files are megabytes each,
so they aren't committed.
