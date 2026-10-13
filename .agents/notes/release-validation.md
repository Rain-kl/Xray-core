# Gate core publication on functional tests

The operator requires custom cores exclusively and expects errors to be detected during development/build. Add one shared Linux validation job rather than repeat tests on every cross-compiled platform. Both main and tagged releases depend on this gate. Preserve existing asset checks and release suffix handling.
