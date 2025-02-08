mkdir bin
GOOS=windows GOARCH=amd64 go build -o bin/test.exe
GOOS=linux GOARCH=amd64 go build -o bin/test
