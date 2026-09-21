# Build every binary once. This image ships awd and aw-policy; the compose
# file runs awd, and aw-sync joins this image in a later milestone.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/awd ./cmd/awd  && CGO_ENABLED=0 go build -o /out/aw-policy ./cmd/aw-policy

FROM alpine:3.20
RUN adduser -D -u 10001 awd
COPY --from=build /out/ /usr/local/bin/
USER awd
EXPOSE 8080
ENTRYPOINT ["awd"]
CMD ["serve"]
