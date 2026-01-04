RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /server ./cmd/host-proxy/main.go
copy ./cmd/host-proxy/config.yaml /config.yaml



docker build -f Dockerfile -t nitro-aes-enclave:latest .
mkdir eif
#build enclave-os
sudo nitro-cli build-enclave --docker-uri nitro-aes-enclave:latest --output-file eif/nitro-aes-enclave.eif

#run server
chmod +x /server

/server & nitro-cli run-enclave --eif-path eif/nitro-aes-enclave.eif --cpu-count 2 --memory 1500 --enclave-cid 16 --debug-mode
