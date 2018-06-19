FROM golang:latest
WORKDIR /go/src/github.com/clustergarage/fim-k8s/
RUN go get -d -v github.com/Sirupsen/logrus \
  k8s.io/client-go/discovery
COPY . /go/src/github.com/clustergarage/fim-k8s/
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=0 /go/src/github.com/clustergarage/fim-k8s/app .
ENTRYPOINT ["./app"]
CMD [""]
