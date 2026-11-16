What this changes for someone using Media Core, and how you checked it:



- [ ] One change in this pull request
- [ ] A line in `CHANGELOG.md` under Unreleased (if a user would notice)
- [ ] Engine: `gofmt`, `go vet` and `go test -race` are clean, and tests use `t.TempDir()`
- [ ] A state file changed shape: there's a migration (`engine/migrate.go`)
- [ ] Nothing private: the pre-commit hook ran (`git config core.hooksPath .githooks`)
