FROM golang:1.16.5 AS build

WORKDIR /
COPY * /
RUN CGO_ENABLED=0 go build -o config-pod . 

FROM scratch

WORKDIR /

COPY --from=build config-pod /config-pod

ENTRYPOINT /config-pod
