FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/proton-mail-tools ./cmd/proton-mail-tools

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/proton-mail-tools /proton-mail-tools

EXPOSE 8930

ENTRYPOINT ["/proton-mail-tools"]
