binary  := "triage"
version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`

build:
    go build -ldflags="-X main.version={{version}}" -o {{binary}} .

install:
    go install -ldflags="-X main.version={{version}}" .

test:
    go test -v -race -count=1 ./...

clean:
    rm -f {{binary}}
    rm -rf triaged-maints/
