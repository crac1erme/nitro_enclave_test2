CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o ./server ./cmd/host-proxy/main.go
cp ./cmd/host-proxy/config.yaml ./config.yaml

chmod +x ./server

./server