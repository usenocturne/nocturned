build:
    GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w" -o nocturned
