FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/orion-operation ./services/orion-operation/cmd/orion-operation

FROM scratch
COPY --from=builder /bin/orion-operation /bin/orion-operation
ENTRYPOINT ["/bin/orion-operation"]
