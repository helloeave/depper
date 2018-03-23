# Depper — Put Lids on your Go Dependencies

[![CircleCI](https://circleci.com/gh/helloeave/depper.svg?style=svg)](https://circleci.com/gh/helloeave/depper)
[![GoDoc](https://godoc.org/github.com/helloeave/depper?status.svg)](https://godoc.org/github.com/helloeave/depper)

Depper is used to express constraints on the go package dependency graph. Like its sibling [lidder](https://github.com/helloeave/lidder), depper works by defining various rules. Here is an example

```
rules:
  - name: utilities can use other utilities and config
    packages: util/.*
    may_depend:
      - <.*>
      - third_parties
      - config
      - .*util
    expected:
      - util/old_and_clunky -> server
      - old_common
```

Each rule applies to a set of `packages`, and you can describe allowed dependencies i.e. `may_depend`, as well as know exceptions i.e. `expected`.

Each `may_depend` entry is a set of packages. It can be
- A specific package, i.e. `foo`; or
- A pattern of packages, i.e. `foo/.*` or `foo_[0-9]`;
- Using `<pattern>` indicates matching against standard library packages; and
- The special `third_parties` matches any third party package

The known exceptions `expected` can be
- Generic e.g. `bar` meaning that in the set of packages, some are known to depend on package `bar`, or
- Specific e.g. `foo -> bar` indicating `foo` is known to depend on `bar`.
