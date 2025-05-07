Shellwords
===========

A golang library for splitting command-line strings into words like a Posix or Windows shell would.

## Installation

```bash
go get -u github.com/buildkite/shellwords
```

## Usage

```go
package main

import (
  "github.com/buildkite/shellwords"
  "fmt"
)

func main() {
  words := shellwords.Split(`/usr/bin/bash -e -c "llamas are the \"best\" && echo 'alpacas'"`)
  for _, word := range words {
    fmt.Println(word)
  }
}

// Outputs:
// /usr/bin/bash
// -e
// -c
// llamas are the "best" && echo 'alpacas'
```

## Alternatives

Previously we were using https://github.com/mattn/go-shellwords, but it lacked support for Windows quoting and escaping conventions, specifically backslashed paths. It also supports things like env expansion and command execution, which made me very nervous, so I decided to write a simpler lexer that just addressed the splitting problem.

Other alternatives are https://github.com/flynn-archive/go-shlex and https://github.com/kballard/go-shellquote, of which the latter is probably closest in spirit to this library.

## License

Licensed under MIT license, in `LICENSE`.
