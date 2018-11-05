FROM golang:1.11.1 as builder
ENV GO111MODULE on
WORKDIR /go/src/clustergarage.io/fim-controller/
# Put dependencies in their own layer so they are cached.
COPY go.mod go.sum ./
RUN go mod download
# Build the rest of the code.
COPY . /go/src/clustergarage.io/fim-controller/
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o fimcontroller .
# Include grpc_health_probe for K8s liveness/readiness checks.
RUN GRPC_HEALTH_PROBE_VERSION=v0.1.0-alpha.1 && \
  wget -qO/bin/grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
  chmod +x /bin/grpc_health_probe

FROM alpine:latest
ENV FIM_CA ""
ENV FIM_CERT ""
ENV FIM_KEY ""
COPY --from=builder /go/src/clustergarage.io/fim-controller/fimcontroller /
COPY --from=builder /go/src/clustergarage.io/fim-controller/docker-entrypoint.sh /
COPY --from=builder /bin/grpc_health_probe /bin/
# glog requires /tmp to exist as log_dir is /tmp by default.
COPY --from=builder /tmp /tmp
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD [""]
