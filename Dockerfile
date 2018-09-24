FROM golang:1.11 as builder
ENV GO111MODULE on
WORKDIR /go/src/clustergarage.io/fim-controller/
COPY . /go/src/clustergarage.io/fim-controller/
RUN go mod download
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o fimcontroller .

FROM scratch
COPY --from=builder /go/src/clustergarage.io/fim-controller/fimcontroller /
# glog requires /tmp to exist as log_dir is /tmp by default
COPY --from=builder /tmp /tmp
CMD ["/fimcontroller"]
