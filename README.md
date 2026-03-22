# go-wavplay

```
GOOS=darwin GOARCH=arm64 go build -o wavplay main.go
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o wavplay main.go
brew install zig
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="zig cc -target x86_64-windows-gnu"  go build -o wavplay.exe main.go
CGO_ENABLED=1 GOOS=windows GOARCH=arm64 CC="zig cc -target aarch64-windows-gnu" go build -o wavplay.exe main.go
```
