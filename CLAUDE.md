# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build/Test Commands

- Build: `go build ./...`
- Test: `go test ./...`
- Test single file: `go test ./path/to/file_test.go`
- Test specific function: `go test -run TestFunctionName`
- Run example: `go run _examples/basic/main.go <server-url>`
- Format code: `go fmt ./...`
- Lint: `golint ./...` or `go vet ./...`

## Code Style Guidelines

- Follow Go standard [style conventions](https://golang.org/doc/effective_go)
- Always use `any` instead of `interface{}`
- Use meaningful package and variable names
- Use CamelCase for exported names, camelCase for non-exported names
- Imports should be grouped: standard library, then third-party, then local
- Proper error handling: check all errors, use `fmt.Errorf` with `%w` for wrapping
- Use context.Context for cancellation and timeouts
- Document all exported types, functions, and methods
- Use sync.Mutex for thread safety and protect shared state
- Prefer strong typing over interface{} where possible
