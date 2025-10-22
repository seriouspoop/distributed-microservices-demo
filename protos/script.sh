#!/bin/bash

# Exit on error
set -e

# Generate user stubs
./bin/protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    userpb/userpb.proto

# Generate billing stubs
./bin/protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    billingpb/billingpb.proto

# Generate notification stubs
./bin/protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    notifpb/notifpb.proto

# Copy generated files to respective services
cp -r userpb ../user-ms/
cp -r userpb ../api-gateway/
cp -r billingpb ../billing-ms/
cp -r billingpb ../api-gateway/
cp -r notifpb ../notification-ms/
cp -r notifpb ../api-gateway/

echo "Protobuf stubs generated and copied successfully."

