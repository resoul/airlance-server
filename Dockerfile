# One static binary, no shell, no package manager, a non-root user.
FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/airlance-server ./cmd/airlance-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/airlance-server /airlance-server
USER nonroot:nonroot
ENTRYPOINT ["/airlance-server"]
CMD ["serve"]
