FROM golang:latest as builder
WORKDIR /go/src/clustergarage.io/fim-controller/
COPY . /go/src/clustergarage.io/fim-controller/
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o fimcontroller .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /go/src/clustergarage.io/fim-controller/fimcontroller .
CMD ["./fimcontroller"]
