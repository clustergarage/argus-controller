FROM golang:1.11 as builder
ENV GO111MODULE on
WORKDIR /go/src/clustergarage.io/fim-controller/
# Put dependencies in their own layer so they are cached.
COPY go.mod go.sum ./
RUN go mod download
# Build the rest of the code.
COPY . /go/src/clustergarage.io/fim-controller/
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o fimcontroller .

FROM scratch
COPY --from=builder /go/src/clustergarage.io/fim-controller/fimcontroller /
# glog requires /tmp to exist as log_dir is /tmp by default
COPY --from=builder /tmp /tmp
CMD ["/fimcontroller"]
